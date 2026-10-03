package service

import (
	"errors"
	"testing"
)

func TestValidateVideoFileName(t *testing.T) {
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}

	tests := []struct {
		in, name, ext, ct string
		bad               bool
	}{
		{in: "lecture.mp4", name: "lecture.mp4", ext: ".mp4", ct: "video/mp4"},
		{in: "  Lecture 1.MOV ", name: "Lecture 1.MOV", ext: ".mov", ct: "video/quicktime"},
		{in: "a.mkv", name: "a.mkv", ext: ".mkv", ct: "video/x-matroska"},
		{in: "a.webm", name: "a.webm", ext: ".webm", ct: "video/webm"},
		{in: "../../etc/passwd.mp4", name: "passwd.mp4", ext: ".mp4", ct: "video/mp4"},
		{in: `C:\videos\clip.mp4`, name: "clip.mp4", ext: ".mp4", ct: "video/mp4"},
		{in: "cl\x00ip\n.mp4", name: "clip.mp4", ext: ".mp4", ct: "video/mp4"},
		{in: "virus.exe", bad: true},
		{in: "noext", bad: true},
		{in: ".mp4/", bad: true},
		{in: "", bad: true},
		{in: "   ", bad: true},
		{in: string(long) + ".mp4", bad: true},
	}
	for _, tt := range tests {
		name, ext, ct, err := validateVideoFileName(tt.in)
		if tt.bad {
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("%q: want ErrInvalidInput, got %v", tt.in, err)
			}
			continue
		}
		if err != nil || name != tt.name || ext != tt.ext || ct != tt.ct {
			t.Errorf("%q: got (%q, %q, %q, %v), want (%q, %q, %q)", tt.in, name, ext, ct, err, tt.name, tt.ext, tt.ct)
		}
	}
}

func TestRewritePlaylist(t *testing.T) {
	in := "#EXTM3U\r\n" +
		"#EXT-X-MEDIA:TYPE=AUDIO,URI=\"audio.m3u8\"\r\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=1\r\n" +
		"media-sd.m3u8\r\n" +
		"#EXTINF:6.000,\r\n" +
		"media-sd0000000000.ts\r\n" +
		"\r\n"
	got := string(rewritePlaylist([]byte(in), func(uri string) string { return "S(" + uri + ")" }))
	want := "#EXTM3U\n" +
		"#EXT-X-MEDIA:TYPE=AUDIO,URI=\"S(audio.m3u8)\"\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=1\n" +
		"S(media-sd.m3u8)\n" +
		"#EXTINF:6.000,\n" +
		"S(media-sd0000000000.ts)\n" +
		"\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestPlaylistNamePattern(t *testing.T) {
	for name, ok := range map[string]bool{
		"manifest.m3u8": true,
		"media-sd.m3u8": true,
		"../x.m3u8":     false,
		"a/b.m3u8":      false,
		"seg.ts":        false,
		"source.mp4":    false,
		".m3u8":         false,
	} {
		if playlistNamePattern.MatchString(name) != ok {
			t.Errorf("%q: want %v", name, ok)
		}
	}
}
