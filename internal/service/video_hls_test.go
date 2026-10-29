package service

import (
	"context"
	"errors"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/storage"
)

// memVideoStore is an in-memory storage.VideoStore for checking uploaded HLS folders.
type memVideoStore struct {
	storage.DisabledVideoStore
	objects map[string]storage.ObjectInfo
	data    map[string][]byte
	reads   int
}

func newMemVideoStore() *memVideoStore {
	return &memVideoStore{objects: map[string]storage.ObjectInfo{}, data: map[string][]byte{}}
}

func (m *memVideoStore) put(name, body string) {
	ct, _ := storage.HLSContentType(path.Ext(name))
	m.objects[name] = storage.ObjectInfo{Name: name, Size: int64(len(body)), ContentType: ct}
	m.data[name] = []byte(body)
}

func (m *memVideoStore) Enabled() bool { return true }

func (m *memVideoStore) ListObjects(_ context.Context, prefix string, max int) ([]storage.ObjectInfo, error) {
	var out []storage.ObjectInfo
	for name, o := range m.objects {
		if strings.HasPrefix(name, prefix) {
			if len(out) == max {
				return nil, storage.ErrTooManyObjects
			}
			out = append(out, o)
		}
	}
	return out, nil
}

func (m *memVideoStore) ReadSmallObject(_ context.Context, object string, maxBytes int64) ([]byte, error) {
	m.reads++
	d, ok := m.data[object]
	if !ok {
		return nil, storage.ErrObjectNotFound
	}
	if int64(len(d)) > maxBytes {
		return nil, errors.New("too large")
	}
	return d, nil
}

const testPrefix = "courses/c/videos/v/hls/"

// putLadder stores what the browser uploads for a two-rendition video with shared audio.
func putLadder(m *memVideoStore) {
	m.put(testPrefix+"manifest.m3u8", `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-1",NAME="audio",DEFAULT=YES,AUTOSELECT=YES,URI="audio.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=3000000,CODECS="avc1.64001f,mp4a.40.2",RESOLUTION=1280x720,AUDIO="audio-1"
720p.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1500000,CODECS="avc1.64001e,mp4a.40.2",RESOLUTION=854x480,AUDIO="audio-1"
480p.m3u8
`)
	for _, r := range []string{"720p", "480p", "audio"} {
		m.put(testPrefix+r+".m3u8", "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MAP:URI=\""+r+"/init.mp4\"\n"+
			"#EXTINF:6,\n"+r+"/seg-00001.m4s\n#EXTINF:2.5,\n"+r+"/seg-00002.m4s\n#EXT-X-ENDLIST\n")
		m.put(testPrefix+r+"/init.mp4", "init")
		m.put(testPrefix+r+"/seg-00001.m4s", "segment-1")
		m.put(testPrefix+r+"/seg-00002.m4s", "segment-2")
	}
}

func TestCheckUploadedHLS(t *testing.T) {
	m := newMemVideoStore()
	putLadder(m)
	m.put(testPrefix+"1080p/seg-00009.m4s", "left over from an earlier attempt")
	m.put("courses/c/videos/other/hls/manifest.m3u8", "another video's file")

	got, err := checkUploadedHLS(context.Background(), m, testPrefix)
	if err != nil {
		t.Fatalf("valid upload rejected: %v", err)
	}
	if got.size <= 0 {
		t.Errorf("size = %d, want > 0", got.size)
	}
	if !slices.Equal(got.stray, []string{testPrefix + "1080p/seg-00009.m4s"}) {
		t.Errorf("stray = %v", got.stray)
	}
	if m.reads != 4 {
		t.Errorf("read %d playlists, want 4 (master, 2 video, audio once)", m.reads)
	}
}

func TestCheckUploadedHLSRejects(t *testing.T) {
	tests := []struct {
		name   string
		change func(m *memVideoStore)
		want   string
	}{
		{"nothing uploaded", func(m *memVideoStore) { clear(m.objects); clear(m.data) }, "not uploaded yet"},
		{"no manifest", func(m *memVideoStore) { delete(m.objects, testPrefix+"manifest.m3u8") }, "not uploaded yet"},
		{"missing segment", func(m *memVideoStore) { delete(m.objects, testPrefix+"480p/seg-00002.m4s") }, "missing 480p/seg-00002.m4s"},
		{"missing init", func(m *memVideoStore) { delete(m.objects, testPrefix+"audio/init.mp4") }, "missing audio/init.mp4"},
		{"missing rendition playlist", func(m *memVideoStore) { delete(m.objects, testPrefix+"720p.m3u8") }, "missing 720p.m3u8"},
		{"rendition not finished", func(m *memVideoStore) {
			m.put(testPrefix+"720p.m3u8", "#EXTM3U\n#EXT-X-MAP:URI=\"720p/init.mp4\"\n#EXTINF:6,\n720p/seg-00001.m4s\n")
		}, "incomplete"},
		{"rendition without segments", func(m *memVideoStore) {
			m.put(testPrefix+"720p.m3u8", "#EXTM3U\n#EXT-X-MAP:URI=\"720p/init.mp4\"\n#EXT-X-ENDLIST\n")
		}, "no segments"},
		{"manifest not a playlist", func(m *memVideoStore) { m.put(testPrefix+"manifest.m3u8", "<html>") }, "not an HLS playlist"},
		{"manifest without renditions", func(m *memVideoStore) { m.put(testPrefix+"manifest.m3u8", "#EXTM3U\n") }, "no renditions"},
		{"manifest points outside", func(m *memVideoStore) { m.put(testPrefix+"manifest.m3u8", "#EXTM3U\n../other/hls/x.m3u8\n") }, "unexpected file"},
		{"manifest points at a segment", func(m *memVideoStore) { m.put(testPrefix+"manifest.m3u8", "#EXTM3U\n720p/seg-00001.m4s\n") }, "unexpected file"},
		{"manifest points at itself", func(m *memVideoStore) { m.put(testPrefix+"manifest.m3u8", "#EXTM3U\nmanifest.m3u8\n") }, "unexpected file"},
		{"segment on another host", func(m *memVideoStore) {
			m.put(testPrefix+"720p.m3u8", "#EXTM3U\n#EXTINF:6,\nhttps://evil.example/seg.m4s\n#EXT-X-ENDLIST\n")
		}, "unexpected file"},
		{"segment outside the folder", func(m *memVideoStore) {
			m.put(testPrefix+"720p.m3u8", "#EXTM3U\n#EXTINF:6,\n../../x/seg.m4s\n#EXT-X-ENDLIST\n")
		}, "unexpected file"},
		{"init outside the folder", func(m *memVideoStore) {
			m.put(testPrefix+"720p.m3u8", "#EXTM3U\n#EXT-X-MAP:URI=\"/abs/init.mp4\"\n#EXTINF:6,\n720p/seg-00001.m4s\n#EXT-X-ENDLIST\n")
		}, "unexpected file"},
		{"nested playlist", func(m *memVideoStore) {
			m.put(testPrefix+"720p.m3u8", "#EXTM3U\n#EXTINF:6,\n720p/x.m3u8\n#EXT-X-ENDLIST\n")
		}, "unexpected file"},
		{"wrong content type", func(m *memVideoStore) {
			o := m.objects[testPrefix+"720p/seg-00001.m4s"]
			o.ContentType = "text/html"
			m.objects[testPrefix+"720p/seg-00001.m4s"] = o
		}, "wrong content type"},
		{"too many files", func(m *memVideoStore) {
			for i := range maxHLSFiles {
				m.objects[testPrefix+"junk/"+time.Duration(i).String()] = storage.ObjectInfo{}
			}
		}, "more than"},
		{"too large", func(m *memVideoStore) {
			o := m.objects[testPrefix+"720p/seg-00001.m4s"]
			o.Size = maxHLSBytes
			m.objects[testPrefix+"720p/seg-00001.m4s"] = o
		}, "larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMemVideoStore()
			putLadder(m)
			tt.change(m)
			_, err := checkUploadedHLS(context.Background(), m, testPrefix)
			if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want ErrInvalidInput containing %q", err, tt.want)
			}
		})
	}
}
