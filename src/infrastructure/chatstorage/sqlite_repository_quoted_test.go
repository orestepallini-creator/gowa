package chatstorage

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func quotedTestContext(t *testing.T, repo *SQLiteRepository, deviceID string) context.Context {
	t.Helper()
	return whatsapp.ContextWithDevice(
		context.Background(),
		whatsapp.NewDeviceInstance(deviceID, nil, repo),
	)
}

func assertQuoted(t *testing.T, got *domainChatStorage.Message, wantID, wantSender, wantContent string) {
	t.Helper()
	if got == nil {
		t.Fatal("expected stored message")
	}
	if got.QuotedMessageID != wantID {
		t.Fatalf("quoted_message_id = %q, want %q", got.QuotedMessageID, wantID)
	}
	if got.QuotedSender != wantSender {
		t.Fatalf("quoted_sender = %q, want %q", got.QuotedSender, wantSender)
	}
	if got.QuotedContent != wantContent {
		t.Fatalf("quoted_content = %q, want %q", got.QuotedContent, wantContent)
	}
}

func TestCreateMessageStoresQuotedReply(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	accountJID := types.NewJID("15550101000", types.DefaultUserServer)
	peerJID := types.NewJID("15550102000", types.DefaultUserServer)
	deviceID := accountJID.String()
	ctx := quotedTestContext(t, repo, deviceID)
	now := time.Date(2026, time.October, 14, 10, 0, 0, 0, time.UTC)

	incoming := func(id string, msg *waE2E.Message, at time.Time) *events.Message {
		return &events.Message{
			Info: types.MessageInfo{
				MessageSource: types.MessageSource{Chat: peerJID, Sender: peerJID},
				ID:            id,
				PushName:      "Alice",
				Timestamp:     at,
			},
			Message: msg,
		}
	}

	textReply := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("Sí, mañana a las 10"),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String("ORIGINAL-1"),
			Participant:   proto.String(accountJID.String()),
			QuotedMessage: &waE2E.Message{Conversation: proto.String("¿Tienes hora mañana?")},
		},
	}}
	imageReply := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption:  proto.String("así quedó"),
		Mimetype: proto.String("image/jpeg"),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:    proto.String("ORIGINAL-2"),
			Participant: proto.String(peerJID.String()),
			QuotedMessage: &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
				Caption: proto.String("antes del tratamiento"),
			}},
		},
	}}
	plain := &waE2E.Message{Conversation: proto.String("hola")}

	for _, evt := range []*events.Message{
		incoming("reply-text", textReply, now),
		incoming("reply-image", imageReply, now.Add(time.Minute)),
		incoming("plain", plain, now.Add(2*time.Minute)),
	} {
		if err := repo.CreateMessage(ctx, evt); err != nil {
			t.Fatalf("create message %s: %v", evt.Info.ID, err)
		}
	}

	got, err := repo.GetMessageByIDAndDevice(deviceID, "reply-text")
	if err != nil {
		t.Fatalf("get reply-text: %v", err)
	}
	assertQuoted(t, got, "ORIGINAL-1", accountJID.String(), "¿Tienes hora mañana?")

	got, err = repo.GetMessageByIDAndDevice(deviceID, "reply-image")
	if err != nil {
		t.Fatalf("get reply-image: %v", err)
	}
	assertQuoted(t, got, "ORIGINAL-2", peerJID.String(), "antes del tratamiento")

	got, err = repo.GetMessageByIDAndDevice(deviceID, "plain")
	if err != nil {
		t.Fatalf("get plain: %v", err)
	}
	assertQuoted(t, got, "", "", "")

	// The chat history listing returns the same fields.
	messages, err := repo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID: deviceID,
		ChatJID:  peerJID.String(),
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	found := false
	for _, m := range messages {
		if m.ID == "reply-text" {
			found = true
			assertQuoted(t, m, "ORIGINAL-1", accountJID.String(), "¿Tienes hora mañana?")
		}
	}
	if !found {
		t.Fatal("reply-text missing from chat history")
	}
}

func TestStoreMessageKeepsQuotedFieldsWhenRewrittenWithoutThem(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	now := time.Date(2026, time.October, 14, 11, 0, 0, 0, time.UTC)

	base := domainChatStorage.Message{
		ID:        "msg-1",
		ChatJID:   chatJID,
		DeviceID:  deviceID,
		Sender:    chatJID,
		Content:   "respuesta",
		Timestamp: now,
	}
	withQuote := base
	withQuote.QuotedMessageID = "ORIGINAL-1"
	withQuote.QuotedSender = deviceID
	withQuote.QuotedContent = "pregunta"
	if err := repo.StoreMessage(&withQuote); err != nil {
		t.Fatalf("store quoted message: %v", err)
	}

	// A later rewrite without reply context (e.g. a resync that lacks it) must not erase it.
	rewrite := base
	rewrite.Content = "respuesta editada"
	if err := repo.StoreMessage(&rewrite); err != nil {
		t.Fatalf("rewrite message: %v", err)
	}
	batchRewrite := base
	if err := repo.StoreMessagesBatch([]*domainChatStorage.Message{&batchRewrite}); err != nil {
		t.Fatalf("batch rewrite message: %v", err)
	}

	got := getMessagesForTest(t, repo, deviceID, chatJID)[0]
	assertQuoted(t, got, "ORIGINAL-1", deviceID, "pregunta")

	// A rewrite that does carry reply context replaces it.
	requote := base
	requote.QuotedMessageID = "ORIGINAL-2"
	requote.QuotedSender = chatJID
	requote.QuotedContent = "otra"
	if err := repo.StoreMessagesBatch([]*domainChatStorage.Message{&requote}); err != nil {
		t.Fatalf("batch requote message: %v", err)
	}
	got = getMessagesForTest(t, repo, deviceID, chatJID)[0]
	assertQuoted(t, got, "ORIGINAL-2", chatJID, "otra")
}

func TestStoreSentMessageWithContextStoresQuotedReply(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "6289605618749@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	ctx := quotedTestContext(t, repo, deviceID)
	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)

	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("te confirmo"),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String("INCOMING-9"),
			Participant:   proto.String("628123456789:12@s.whatsapp.net"),
			QuotedMessage: &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Caption: proto.String("presupuesto.pdf")}},
		},
	}}
	if err := repo.StoreSentMessageWithContext(ctx, "sent-1", deviceID, chatJID, "te confirmo", now, msg); err != nil {
		t.Fatalf("store sent message: %v", err)
	}

	got, err := repo.GetMessageByIDAndDevice(deviceID, "sent-1")
	if err != nil {
		t.Fatalf("get sent message: %v", err)
	}
	// The device suffix is stripped, like message senders.
	assertQuoted(t, got, "INCOMING-9", chatJID, "presupuesto.pdf")
}

func TestInitializeSchemaAddsQuotedColumnsToExistingDatabase(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	migrations := repo.getMigrations()
	firstQuoted := len(migrations) - 2 // version of the quoted_message_id migration

	// Rewind the database to the schema before the quoted columns existed.
	for _, column := range []string{"quoted_content", "quoted_sender", "quoted_message_id"} {
		if _, err := repo.db.Exec("ALTER TABLE messages DROP COLUMN " + column); err != nil {
			t.Fatalf("drop %s: %v", column, err)
		}
	}
	if _, err := repo.db.Exec("DELETE FROM schema_info WHERE version >= ?", firstQuoted); err != nil {
		t.Fatalf("rewind schema_info: %v", err)
	}
	now := time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
	// Same column list the repository used before this change.
	if _, err := repo.db.Exec(`
		INSERT INTO messages (
			id, chat_jid, device_id, sender, content, timestamp, is_from_me,
			media_type, call_metadata, filename, url, direct_path, media_key, file_sha256,
			file_enc_sha256, file_length, referral_metadata, created_at, updated_at
		) VALUES ('legacy-1', 'chat@s.whatsapp.net', 'dev@s.whatsapp.net', 'chat@s.whatsapp.net', 'old row', ?, 0,
			'', '', '', '', '', NULL, NULL, NULL, 0, '', ?, ?)
	`, now, now, now); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("re-run migrations: %v", err)
	}
	version, err := repo.getSchemaVersion()
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("schema version = %d, want %d", version, len(migrations))
	}

	got, err := repo.GetMessageByIDAndDevice("dev@s.whatsapp.net", "legacy-1")
	if err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if got == nil || got.Content != "old row" {
		t.Fatalf("legacy row = %#v", got)
	}
	assertQuoted(t, got, "", "", "")
}
