package transcription

import (
	"testing"
	"time"
)

func TestParseJSONReadsTimestampedSegmentsFromWhisperOutput(t *testing.T) {
	segments, err := parseJSON([]byte(`{"result":{"transcription":[{"timestamps":{"from":"00:00:01,250","to":"00:00:03,500"},"text":" Deploy the service. "}]}}`))
	if err != nil {
		t.Fatalf("parseJSON() error = %v", err)
	}
	if got, want := len(segments), 1; got != want {
		t.Fatalf("segments = %#v, want one segment", segments)
	}
	if got, want := segments[0], (Segment{Start: 1250 * time.Millisecond, End: 3500 * time.Millisecond, Text: "Deploy the service."}); got != want {
		t.Errorf("segment = %#v, want %#v", got, want)
	}
}
