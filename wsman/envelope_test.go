package wsman

import (
	"os"
	"path/filepath"
	"testing"
)

// updateGolden を true にすると Golden ファイルを更新する
const updateGolden = false

func loadGolden(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Golden ファイルの読み込みに失敗: %v", err)
	}
	return data
}

func saveGolden(t *testing.T, name string, data []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("Golden ファイルの書き込みに失敗: %v", err)
	}
}

func TestEnvelope_MarshalXML(t *testing.T) {
	tests := []struct {
		name   string
		env    *Envelope
		golden string
	}{
		{
			name:   "空のエンベロープ",
			env:    NewEnvelope(),
			golden: "envelope_empty.xml",
		},
		{
			name:   "Action ヘッダー付きエンベロープ",
			env:    NewEnvelope(WithAction(ActionGet)),
			golden: "envelope_get_action.xml",
		},
		{
			name: "複数ヘッダー付きエンベロープ",
			env: NewEnvelope(
				WithAction(ActionGet),
				WithResourceURI("http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem"),
				WithMaxEnvelopeSize(153600),
				WithOperationTimeout("PT60S"),
			),
			golden: "envelope_full_headers.xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalEnvelope(tt.env)
			if err != nil {
				t.Fatalf("MarshalEnvelope に失敗: %v", err)
			}

			if updateGolden {
				saveGolden(t, tt.golden, got)
				return
			}

			want := loadGolden(t, tt.golden)
			if string(got) != string(want) {
				t.Errorf("XML が Golden ファイルと一致しません\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

func TestEnvelope_UnmarshalXML(t *testing.T) {
	t.Run("Action ヘッダー付きエンベロープをパース", func(t *testing.T) {
		data := loadGolden(t, "envelope_get_action.xml")

		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("UnmarshalEnvelope に失敗: %v", err)
		}

		if env.Header.Action == nil {
			t.Fatal("Action ヘッダーが nil")
		}
		if env.Header.Action.Value != ActionGet {
			t.Errorf("Action = %q, want %q", env.Header.Action.Value, ActionGet)
		}
		if env.Header.Action.MustUnderstand != "true" {
			t.Errorf("MustUnderstand = %q, want %q", env.Header.Action.MustUnderstand, "true")
		}
	})

	t.Run("複数ヘッダー付きエンベロープをパース", func(t *testing.T) {
		data := loadGolden(t, "envelope_full_headers.xml")

		env, err := UnmarshalEnvelope(data)
		if err != nil {
			t.Fatalf("UnmarshalEnvelope に失敗: %v", err)
		}

		if env.Header.Action == nil {
			t.Fatal("Action ヘッダーが nil")
		}
		if env.Header.Action.Value != ActionGet {
			t.Errorf("Action = %q, want %q", env.Header.Action.Value, ActionGet)
		}

		if env.Header.ResourceURI == nil {
			t.Fatal("ResourceURI ヘッダーが nil")
		}
		wantURI := "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/cimv2/Win32_OperatingSystem"
		if env.Header.ResourceURI.Value != wantURI {
			t.Errorf("ResourceURI = %q, want %q", env.Header.ResourceURI.Value, wantURI)
		}

		if env.Header.MaxEnvelopeSize == nil {
			t.Fatal("MaxEnvelopeSize ヘッダーが nil")
		}
		if env.Header.MaxEnvelopeSize.Value != 153600 {
			t.Errorf("MaxEnvelopeSize = %d, want %d", env.Header.MaxEnvelopeSize.Value, 153600)
		}

		if env.Header.OperationTimeout == nil {
			t.Fatal("OperationTimeout ヘッダーが nil")
		}
		if env.Header.OperationTimeout.Value != "PT60S" {
			t.Errorf("OperationTimeout = %q, want %q", env.Header.OperationTimeout.Value, "PT60S")
		}
	})
}

func TestNewEnvelope_Options(t *testing.T) {
	t.Run("WithSelector で SelectorSet を構築", func(t *testing.T) {
		env := NewEnvelope(
			WithSelector("Name", "TestService"),
			WithSelector("__cimnamespace", "root/cimv2"),
		)

		if env.Header.SelectorSet == nil {
			t.Fatal("SelectorSet が nil")
		}
		if len(env.Header.SelectorSet.Selectors) != 2 {
			t.Fatalf("Selector 数 = %d, want 2", len(env.Header.SelectorSet.Selectors))
		}
		if env.Header.SelectorSet.Selectors[0].Name != "Name" {
			t.Errorf("Selector[0].Name = %q, want %q", env.Header.SelectorSet.Selectors[0].Name, "Name")
		}
		if env.Header.SelectorSet.Selectors[0].Value != "TestService" {
			t.Errorf("Selector[0].Value = %q, want %q", env.Header.SelectorSet.Selectors[0].Value, "TestService")
		}
	})

	t.Run("WithTo と WithReplyTo", func(t *testing.T) {
		env := NewEnvelope(
			WithTo("http://host:5986/wsman"),
			WithReplyTo(AddressAnonymous),
		)

		if env.Header.To == nil {
			t.Fatal("To が nil")
		}
		if env.Header.To.Value != "http://host:5986/wsman" {
			t.Errorf("To = %q, want %q", env.Header.To.Value, "http://host:5986/wsman")
		}

		if env.Header.ReplyTo == nil {
			t.Fatal("ReplyTo が nil")
		}
		if env.Header.ReplyTo.Address.Value != AddressAnonymous {
			t.Errorf("ReplyTo.Address = %q, want %q", env.Header.ReplyTo.Address.Value, AddressAnonymous)
		}
	})

	t.Run("WithMessageID", func(t *testing.T) {
		env := NewEnvelope(
			WithMessageID("uuid:test-id-123"),
		)

		if env.Header.MessageID == nil {
			t.Fatal("MessageID が nil")
		}
		if env.Header.MessageID.Value != "uuid:test-id-123" {
			t.Errorf("MessageID = %q, want %q", env.Header.MessageID.Value, "uuid:test-id-123")
		}
	})
}
