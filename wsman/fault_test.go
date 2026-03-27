package wsman

import (
	"testing"
)

func TestFault_Parse(t *testing.T) {
	tests := []struct {
		name       string
		golden     string
		wantCode   string
		wantSub    string
		wantReason string
	}{
		{
			name:       "AccessDenied Fault",
			golden:     "fault_access_denied.xml",
			wantCode:   "s:Sender",
			wantSub:    "w:AccessDenied",
			wantReason: "Access is denied.",
		},
		{
			name:       "InvalidParameter Fault",
			golden:     "fault_invalid_parameter.xml",
			wantCode:   "s:Sender",
			wantSub:    "w:InvalidParameter",
			wantReason: "The parameter is incorrect.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := loadGolden(t, tt.golden)

			fault, err := ParseFault(data)
			if err != nil {
				t.Fatalf("ParseFault に失敗: %v", err)
			}

			if fault.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", fault.Code, tt.wantCode)
			}
			if fault.Subcode != tt.wantSub {
				t.Errorf("Subcode = %q, want %q", fault.Subcode, tt.wantSub)
			}
			if fault.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", fault.Reason, tt.wantReason)
			}
		})
	}
}

func TestFault_Error(t *testing.T) {
	fault := &Fault{
		Code:    "s:Sender",
		Subcode: "w:AccessDenied",
		Reason:  "Access is denied.",
	}

	errMsg := fault.Error()
	if errMsg == "" {
		t.Fatal("Error() が空文字列を返した")
	}

	// error インターフェースを満たすことを確認
	var err error = fault
	if err.Error() == "" {
		t.Fatal("error インターフェースが正しく実装されていない")
	}
}

func TestIsFault_検出(t *testing.T) {
	t.Run("Fault を含む SOAP レスポンス", func(t *testing.T) {
		data := loadGolden(t, "fault_access_denied.xml")
		if !IsFault(data) {
			t.Error("IsFault が false を返した、true が期待される")
		}
	})

	t.Run("正常な SOAP レスポンス", func(t *testing.T) {
		data := loadGolden(t, "envelope_get_action.xml")
		if IsFault(data) {
			t.Error("IsFault が true を返した、false が期待される")
		}
	})
}
