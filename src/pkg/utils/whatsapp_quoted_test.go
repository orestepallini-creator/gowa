package utils

import (
	"strings"
	"testing"
	"unicode/utf8"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestExtractQuotedInfo(t *testing.T) {
	quotedCtx := func(quoted *waE2E.Message) *waE2E.ContextInfo {
		return &waE2E.ContextInfo{
			StanzaID:      proto.String("QUOTED-1"),
			Participant:   proto.String("628123456789@s.whatsapp.net"),
			QuotedMessage: quoted,
		}
	}

	tests := []struct {
		name string
		msg  *waE2E.Message
		want QuotedInfo
	}{
		{name: "Nil", msg: nil, want: QuotedInfo{}},
		{name: "PlainConversation", msg: &waE2E.Message{Conversation: proto.String("hola")}, want: QuotedInfo{}},
		{
			name: "ForwardedWithoutStanza",
			msg: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String("fwd"),
				ContextInfo: &waE2E.ContextInfo{IsForwarded: proto.Bool(true)},
			}},
			want: QuotedInfo{},
		},
		{
			name: "ExtendedTextQuotingConversation",
			msg: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String("sí"),
				ContextInfo: quotedCtx(&waE2E.Message{Conversation: proto.String("¿vienes?")}),
			}},
			want: QuotedInfo{MessageID: "QUOTED-1", Sender: "628123456789@s.whatsapp.net", Content: "¿vienes?"},
		},
		{
			name: "AudioQuotingExtendedText",
			msg: &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
				ContextInfo: quotedCtx(&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("mira el link")}}),
			}},
			want: QuotedInfo{MessageID: "QUOTED-1", Sender: "628123456789@s.whatsapp.net", Content: "mira el link"},
		},
		{
			name: "StickerQuotingImageCaption",
			msg: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
				ContextInfo: quotedCtx(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("foto")}}),
			}},
			want: QuotedInfo{MessageID: "QUOTED-1", Sender: "628123456789@s.whatsapp.net", Content: "foto"},
		},
		{
			name: "DocumentQuotingVideoCaption",
			msg: &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
				ContextInfo: quotedCtx(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: proto.String("video")}}),
			}},
			want: QuotedInfo{MessageID: "QUOTED-1", Sender: "628123456789@s.whatsapp.net", Content: "video"},
		},
		{
			name: "EphemeralWrappedImageQuotingDocumentCaption",
			msg: &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
				ImageMessage: &waE2E.ImageMessage{
					Caption:     proto.String("respuesta"),
					ContextInfo: quotedCtx(&waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Caption: proto.String("contrato")}}),
				},
			}}},
			want: QuotedInfo{MessageID: "QUOTED-1", Sender: "628123456789@s.whatsapp.net", Content: "contrato"},
		},
		{
			name: "QuotedMessageMissing",
			msg: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String("ok"),
				ContextInfo: quotedCtx(nil),
			}},
			want: QuotedInfo{MessageID: "QUOTED-1", Sender: "628123456789@s.whatsapp.net"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractQuotedInfo(tt.msg); got != tt.want {
				t.Fatalf("ExtractQuotedInfo() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestExtractQuotedInfoTruncatesContent(t *testing.T) {
	long := strings.Repeat("ñ", MaxQuotedContentLength+50)
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("ok"),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String("QUOTED-LONG"),
			QuotedMessage: &waE2E.Message{Conversation: proto.String(long)},
		},
	}}

	got := ExtractQuotedInfo(msg)
	if n := utf8.RuneCountInString(got.Content); n != MaxQuotedContentLength {
		t.Fatalf("quoted content length = %d runes, want %d", n, MaxQuotedContentLength)
	}
	if !utf8.ValidString(got.Content) {
		t.Fatal("truncated quoted content is not valid UTF-8")
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"", 5, ""},
		{"hola", 10, "hola"},
		{"hola", 4, "hola"},
		{"hola", 2, "ho"},
		{"mañana", 3, "mañ"},
		{"abc", 0, ""},
	}
	for _, tt := range tests {
		if got := TruncateRunes(tt.in, tt.max); got != tt.want {
			t.Fatalf("TruncateRunes(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
	}
}
