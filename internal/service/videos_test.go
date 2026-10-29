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
		in, name string
		bad      bool
	}{
		{in: "lecture.mp4", name: "lecture.mp4"},
		{in: "  Lecture 1.MOV ", name: "Lecture 1.MOV"},
		{in: "a.mkv", name: "a.mkv"},
		{in: "a.webm", name: "a.webm"},
		{in: "../../etc/passwd.mp4", name: "passwd.mp4"},
		{in: `C:\videos\clip.mp4`, name: "clip.mp4"},
		{in: "cl\x00ip\n.mp4", name: "clip.mp4"},
		{in: "virus.exe", bad: true},
		{in: "noext", bad: true},
		{in: ".mp4/", bad: true},
		{in: "", bad: true},
		{in: "   ", bad: true},
		{in: string(long) + ".mp4", bad: true},
	}
	for _, tt := range tests {
		name, err := validateVideoFileName(tt.in)
		if tt.bad {
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("%q: want ErrInvalidInput, got %v", tt.in, err)
			}
			continue
		}
		if err != nil || name != tt.name {
			t.Errorf("%q: got (%q, %v), want %q", tt.in, name, err, tt.name)
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

func TestAddStartTime(t *testing.T) {
	got := string(addStartTime([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1\nmedia-sd.m3u8\n"), 754))
	want := "#EXTM3U\n#EXT-X-START:TIME-OFFSET=754,PRECISE=YES\n#EXT-X-STREAM-INF:BANDWIDTH=1\nmedia-sd.m3u8\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if got := string(addStartTime([]byte("not a playlist"), 754)); got != "not a playlist" {
		t.Errorf("non-playlist changed: %q", got)
	}
}

func TestValidateVideoDescription(t *testing.T) {
	ok := make([]rune, maxVideoDescriptionChars)
	for i := range ok {
		ok[i] = 'é' // multi-byte: the limit counts characters, not bytes
	}
	if err := validateVideoDescription(""); err != nil {
		t.Errorf("empty description: %v", err)
	}
	if err := validateVideoDescription(string(ok)); err != nil {
		t.Errorf("%d characters: %v", maxVideoDescriptionChars, err)
	}
	if err := validateVideoDescription(string(ok) + "x"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("%d characters: got %v, want ErrInvalidInput", maxVideoDescriptionChars+1, err)
	}
}
