package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	whatsmeow_types "go.mau.fi/whatsmeow/types"
)

// Cache em memoria do timer de mensagens temporarias por instancia+chat.
// Preenchido a partir das mensagens que chegam/saem; usado no envio para
// evitar o aviso "ocorreu um conflito entre as duracoes da mensagem temporaria".
// Gravado em disco (volume /app/dbdata) para sobreviver a reinicios.
var (
	ephMu    sync.Mutex
	ephData  = map[string]uint32{} // key: instanceId|user -> segundos
	ephOnce  sync.Once
	ephDirty bool
)

func ephFile() string {
	if f := os.Getenv("EPHEMERAL_FILE"); f != "" {
		return f
	}
	return "/app/dbdata/ephemeral_timers.json"
}

func ephLoad() {
	ephOnce.Do(func() {
		if b, err := os.ReadFile(ephFile()); err == nil {
			_ = json.Unmarshal(b, &ephData)
		}
		go func() {
			for range time.Tick(5 * time.Second) {
				ephSave()
			}
		}()
	})
}

func ephSave() {
	ephMu.Lock()
	if !ephDirty {
		ephMu.Unlock()
		return
	}
	b, err := json.Marshal(ephData)
	ephDirty = false
	ephMu.Unlock()
	if err != nil {
		return
	}
	f := ephFile()
	_ = os.MkdirAll(filepath.Dir(f), 0o755)
	tmp := f + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, f)
	}
}

func ephKey(instanceId, user string) string { return instanceId + "|" + user }

// SetChatExpiration grava o timer (0 = desligado) para os JIDs informados.
func SetChatExpiration(instanceId string, exp uint32, jids ...whatsmeow_types.JID) {
	ephLoad()
	ephMu.Lock()
	defer ephMu.Unlock()
	for _, j := range jids {
		if j.IsEmpty() || j.User == "" {
			continue
		}
		k := ephKey(instanceId, j.User)
		if old, ok := ephData[k]; !ok || old != exp {
			ephData[k] = exp
			ephDirty = true
		}
	}
}

// GetChatExpiration devolve o timer conhecido para algum dos JIDs.
func GetChatExpiration(instanceId string, jids ...whatsmeow_types.JID) (uint32, bool) {
	ephLoad()
	ephMu.Lock()
	defer ephMu.Unlock()
	for _, j := range jids {
		if j.IsEmpty() || j.User == "" {
			continue
		}
		if v, ok := ephData[ephKey(instanceId, j.User)]; ok {
			return v, true
		}
	}
	return 0, false
}

// MessageExpiration extrai o timer de uma mensagem recebida.
// Retorna ok=false quando a mensagem nao traz informacao.
func MessageExpiration(m *waE2E.Message) (uint32, bool) {
	if m == nil {
		return 0, false
	}
	if m.GetEphemeralMessage() != nil {
		m = m.GetEphemeralMessage().GetMessage()
		if m == nil {
			return 0, false
		}
	}
	if pm := m.GetProtocolMessage(); pm != nil && pm.GetType() == waE2E.ProtocolMessage_EPHEMERAL_SETTING {
		return pm.GetEphemeralExpiration(), true
	}
	var ci *waE2E.ContextInfo
	switch {
	case m.GetExtendedTextMessage() != nil:
		ci = m.GetExtendedTextMessage().GetContextInfo()
	case m.GetImageMessage() != nil:
		ci = m.GetImageMessage().GetContextInfo()
	case m.GetVideoMessage() != nil:
		ci = m.GetVideoMessage().GetContextInfo()
	case m.GetAudioMessage() != nil:
		ci = m.GetAudioMessage().GetContextInfo()
	case m.GetDocumentMessage() != nil:
		ci = m.GetDocumentMessage().GetContextInfo()
	case m.GetStickerMessage() != nil:
		ci = m.GetStickerMessage().GetContextInfo()
	case m.GetConversation() != "":
		// texto simples nao tem ContextInfo: chat sem timer
		return 0, true
	default:
		return 0, false
	}
	if ci == nil {
		return 0, true
	}
	return ci.GetExpiration(), true
}

// ApplyExpiration coloca o timer no ContextInfo da mensagem que sera enviada.
func ApplyExpiration(m *waE2E.Message, exp uint32) {
	if m == nil || exp == 0 {
		return
	}
	set := func(ci **waE2E.ContextInfo) {
		if *ci == nil {
			*ci = &waE2E.ContextInfo{}
		}
		e := exp
		(*ci).Expiration = &e
	}
	switch {
	case m.ExtendedTextMessage != nil:
		set(&m.ExtendedTextMessage.ContextInfo)
	case m.ImageMessage != nil:
		set(&m.ImageMessage.ContextInfo)
	case m.VideoMessage != nil:
		set(&m.VideoMessage.ContextInfo)
	case m.PtvMessage != nil:
		set(&m.PtvMessage.ContextInfo)
	case m.AudioMessage != nil:
		set(&m.AudioMessage.ContextInfo)
	case m.DocumentMessage != nil:
		set(&m.DocumentMessage.ContextInfo)
	case m.DocumentWithCaptionMessage != nil && m.DocumentWithCaptionMessage.Message != nil && m.DocumentWithCaptionMessage.Message.DocumentMessage != nil:
		set(&m.DocumentWithCaptionMessage.Message.DocumentMessage.ContextInfo)
	case m.StickerMessage != nil:
		set(&m.StickerMessage.ContextInfo)
	case m.LocationMessage != nil:
		set(&m.LocationMessage.ContextInfo)
	case m.ContactMessage != nil:
		set(&m.ContactMessage.ContextInfo)
	case m.PollCreationMessage != nil:
		set(&m.PollCreationMessage.ContextInfo)
	}
}
