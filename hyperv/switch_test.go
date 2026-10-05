package hyperv

import (
	"context"
	"strings"
	"testing"
)

// TestClient_ListExternalEthernetPorts は物理 NIC 一覧取得を検証する。
func TestClient_ListExternalEthernetPorts(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_externalethernetport.xml")
	pull := loadGolden(t, "pull_response_externalethernetport.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListExternalEthernetPorts(context.Background())
	if err != nil {
		t.Fatalf("ListExternalEthernetPorts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2", len(got))
	}
	if got[0].ElementName != "Realtek Gaming 2.5GbE" {
		t.Errorf("ElementName: %q", got[0].ElementName)
	}
	if got[0].IsBound {
		t.Errorf("IsBound[0]: want false")
	}
	if !got[1].IsBound {
		t.Errorf("IsBound[1]: want true")
	}
}

// TestClient_CreateSwitch_Private は Private Switch 作成リクエストを検証する。
//
// Private は ResourceSettings なし。リクエストには SystemSettings のみ。
func TestClient_CreateSwitch_Private(t *testing.T) {
	resp := loadGolden(t, "invoke_response_define_switch.xml")

	var bodies []string
	server := newSequenceServer(t, []string{resp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
		Name: "PrivateSwitch",
		Type: SwitchTypePrivate,
	})
	if err != nil {
		t.Fatalf("CreateSwitch: %v", err)
	}
	if got.SwitchRef == "" {
		t.Errorf("SwitchRef should not be empty")
	}

	body := bodies[0]
	if !strings.Contains(body, "DefineSystem") {
		t.Errorf("body should call DefineSystem")
	}
	if !strings.Contains(body, "PrivateSwitch") {
		t.Errorf("body should contain switch name")
	}
	// Private は ResourceSettings 0 個
	if strings.Contains(body, "<p:ResourceSettings>") {
		t.Errorf("Private switch body should not contain ResourceSettings element")
	}
}

// TestClient_CreateSwitch_Internal は Internal Switch 作成を検証する。
//
// Internal は ResourceSettings に Internal Port 1 個。その HostResource は
// **ホスト側 Msvm_ComputerSystem の WMI オブジェクトパス**で、配列で送る (#178)。
//
// ホスト側 Msvm_ComputerSystem の取得には**ホストを含む列挙応答**が要るので、
// 実機記録 (recorded_computersystem_*) を使う (#167)。
// 手書き golden でホスト行を足すのは関所が意図的に禁じている形。
//
// 想定リクエスト順 (6 件):
//
//	1-5: listComputerSystemsIncludingHost (enum + pull ×4) — hostComputerSystem 内
//	6: DefineSystem invoke
func TestClient_CreateSwitch_Internal(t *testing.T) {
	resp := loadGolden(t, "invoke_response_define_switch.xml")

	responses := append(recordedComputerSystemSequence(t), resp)
	var bodies []string
	server := newSequenceServer(t, responses, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
		Name: "InternalSwitch",
		Type: SwitchTypeInternal,
	})
	if err != nil {
		t.Fatalf("CreateSwitch: %v", err)
	}

	if len(bodies) != 6 {
		t.Fatalf("expected 6 requests, got %d", len(bodies))
	}

	body := bodies[5]
	if strings.Count(body, "<p:ResourceSettings>") != 1 {
		t.Errorf("Internal switch body should contain exactly 1 ResourceSettings, body=%s", body)
	}

	// HostResource はホスト側 Msvm_ComputerSystem の WMI パスを PROPERTY.ARRAY で。
	// 2026-10-05 実機: 無ければ ErrorCode=32773、スカラーなら 32776 (#178)。
	inst := embeddedInstanceOf(t, unescapeForAssert(body), "Msvm_EthernetPortAllocationSettingData")
	const wantHostResource = `<PROPERTY.ARRAY NAME="HostResource" TYPE="string"><VALUE.ARRAY>` +
		`<VALUE>root/virtualization/v2:Msvm_ComputerSystem.` +
		`CreationClassName="Msvm_ComputerSystem",Name="` + recordedHostName + `"</VALUE>` +
		`</VALUE.ARRAY></PROPERTY.ARRAY>`
	if !strings.Contains(inst, wantHostResource) {
		t.Errorf("Internal Port の HostResource が一致しない。"+
			"欠けると実機は ErrorCode=32773、スカラーなら 32776 (#178)\n got:  %s\n want (部分): %s",
			inst, wantHostResource)
	}
	// 旧実装は HostResource を一切入れていなかった。
	if strings.Contains(inst, `<PROPERTY NAME="HostResource"`) {
		t.Errorf("HostResource がスカラー PROPERTY になっている。実機は ErrorCode=32776 (#178)\n%s", inst)
	}
}

// TestClient_CreateSwitch_External は External Switch 作成を検証する。
//
// 想定リクエスト順 (9 件):
//
//	1-3: ListExternalEthernetPorts (enum + 実機記録 + 終端の legacy pull)
//	4-8: listComputerSystemsIncludingHost (enum + pull ×4) — Internal Port 用 (#178)
//	9: DefineSystem invoke
//
// AllowManagementOS=true なら ResourceSettings は 2 個 (External binding + Internal port)。
//
// ⚠️ **External スイッチ作成は実機で end-to-end 検証できていない** (#146)。
// 検証環境の物理 NIC は 1 枚で既存スイッチに束ねられており (IsBound=true)、
// 束ねられた NIC への再バインドはキーが正しくても ErrorCode=32773 になるため、
// 作成の成否で実装の正しさを判定できない。
//
// **このテストが固定しているのはキーの選び方と形式だけ。**
//   - キーが MOF の 4 つであることの根拠: CIM_LogicalDevice の Key 修飾子と、
//     記録 (recorded_pull_externalethernetport.xml) にその 4 つが実在すること。
//     同記録の Name は ElementName と同値で、Name がキーでないことの裏にもなる
//   - WMI パス化と配列化の根拠: #114 / #178 の実機検証 (別クラスだが同じプロパティ)
//
// 実機が External binding の HostResource にこの形を保存していること自体は、
// プローブで 1 度観測して Issue #146 に貼ってあるだけで、**リポジトリ内に記録は無い**。
// 記録器が実機に到達できない環境問題 (CLAUDE.md「環境固有の注意点」) のため、
// 同じ接続で記録を取れていない。
func TestClient_CreateSwitch_External(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_externalethernetport.xml")
	// 実機記録。MOF のキー 4 つを持つ。EndOfSequence を持たないので、
	// 列挙を終わらせる 2 枚目として legacy golden を続ける (その中身は
	// ElementName が違うので選択されない)。
	recorded := loadGolden(t, "recorded_pull_externalethernetport.xml")
	tail := loadGolden(t, "pull_response_externalethernetport.xml")
	resp := loadGolden(t, "invoke_response_define_switch.xml")

	responses := append([]string{enum, recorded, tail}, recordedComputerSystemSequence(t)...)
	responses = append(responses, resp)
	var bodies []string
	server := newSequenceServer(t, responses, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
		Name:              "ExternalSwitch",
		Type:              SwitchTypeExternal,
		ExternalAdapter:   "Intel(R) Ethernet Connection (2) I218-LM",
		AllowManagementOS: true,
	})
	if err != nil {
		t.Fatalf("CreateSwitch: %v", err)
	}

	if len(bodies) != 9 {
		t.Fatalf("expected 9 requests, got %d", len(bodies))
	}

	invokeBody := bodies[8]
	// AllowManagementOS=true → External binding + Internal Port = 2 個
	if strings.Count(invokeBody, "<p:ResourceSettings>") != 2 {
		t.Errorf("External switch with AllowManagementOS should have 2 ResourceSettings")
	}

	// 2 件とも見る。1 件目が External binding、2 件目が Internal Port。
	insts := embeddedInstancesOf(t, unescapeForAssert(invokeBody),
		"Msvm_EthernetPortAllocationSettingData")
	if len(insts) != 2 {
		t.Fatalf("embedded instance が 2 件でない: %d 件", len(insts))
	}

	// External binding: HostResource は MOF のキー 4 つで組んだ WMI パス。
	// Name は**キーではない**ので入らない (#146)。キー名は昇順。
	const wantExternal = `<PROPERTY.ARRAY NAME="HostResource" TYPE="string"><VALUE.ARRAY>` +
		`<VALUE>root/virtualization/v2:Msvm_ExternalEthernetPort.` +
		`CreationClassName="Msvm_ExternalEthernetPort",` +
		`DeviceID="Microsoft:{00000000-0000-4000-8000-000000000007}",` +
		`SystemCreationClassName="Msvm_ComputerSystem",` +
		`SystemName="scrubbed-1"</VALUE>` +
		`</VALUE.ARRAY></PROPERTY.ARRAY>`
	if !strings.Contains(insts[0], wantExternal) {
		t.Errorf("External binding の HostResource が一致しない (#146)\n got:  %s\n want (部分): %s",
			insts[0], wantExternal)
	}
	// Internal Port 側はホスト CS を指す (#178)。
	if !strings.Contains(insts[1], `Name="`+recordedHostName+`"`) ||
		!strings.Contains(insts[1], "Msvm_ComputerSystem") {
		t.Errorf("Internal Port の HostResource がホスト CS を指していない (#178)\n%s", insts[1])
	}
	// EPR の痕跡が残っていないこと。
	for i, inst := range insts {
		if strings.Contains(inst, "ResourceURI") {
			t.Errorf("instance[%d] に EPR の痕跡がある。HostResource は WMI パスで送る (#146)\n%s", i, inst)
		}
	}
}

// TestClient_CreateSwitch_External_MissingKeys は、応答に MOF のキーが欠けている場合に
// 明示的なエラーになることを検証する。
//
// legacy golden (pull_response_externalethernetport.xml) は SystemName /
// SystemCreationClassName / CreationClassName を持たない。キー欠落に気付かず
// 空文字で WMI パスを組むと、実機は原因の分からない ErrorCode=32773 を返す。
// 手元で落とす方が切り分けが早い (#146)。
func TestClient_CreateSwitch_External_MissingKeys(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_externalethernetport.xml")
	pull := loadGolden(t, "pull_response_externalethernetport.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
		Name:            "ExternalSwitch",
		Type:            SwitchTypeExternal,
		ExternalAdapter: "Realtek Gaming 2.5GbE",
	})
	if err == nil {
		t.Fatal("SystemName が無い応答なのにエラーにならない")
	}
	if !strings.Contains(err.Error(), "missing MOF keys") {
		t.Errorf("キー欠落だと分かるエラーでない: %v", err)
	}
	// DefineSystem まで到達していないこと (列挙 2 件で止まる)。
	if len(bodies) != 2 {
		t.Errorf("DefineSystem を呼んでしまっている: %d 件", len(bodies))
	}
}

// TestClient_CreateSwitch_External_NotFound は ExternalAdapter が存在しないときのエラー。
func TestClient_CreateSwitch_External_NotFound(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_externalethernetport.xml")
	pull := loadGolden(t, "pull_response_externalethernetport.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
		Name:            "ExternalSwitch",
		Type:            SwitchTypeExternal,
		ExternalAdapter: "NonExistent NIC",
	})
	if err == nil {
		t.Fatal("expected error for non-existent external adapter")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found: %v", err)
	}
}

// TestClient_CreateSwitch_Validation はバリデーション。
func TestClient_CreateSwitch_Validation(t *testing.T) {
	client, _ := NewClient("http://localhost")

	if _, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{Type: SwitchTypePrivate}); err == nil {
		t.Error("expected error for empty Name")
	}
	if _, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{Name: "x"}); err == nil {
		t.Error("expected error for empty Type")
	}
	if _, err := client.CreateSwitch(context.Background(), CreateSwitchOptions{
		Name: "x", Type: SwitchTypeExternal,
	}); err == nil {
		t.Error("expected error for missing ExternalAdapter")
	}
}

// TestClient_DestroySwitch は削除リクエストの組み立てを検証する。
//
// 想定: GetVirtualEthernetSwitch (List → Filter) + DestroySystem invoke = 3 リクエスト。
func TestClient_DestroySwitch(t *testing.T) {
	swEnum := loadGolden(t, "enumerate_response_virtualethernetswitch.xml")
	swPull := loadGolden(t, "pull_response_virtualethernetswitch.xml")
	resp := loadGolden(t, "invoke_response_destroy_switch.xml")

	var bodies []string
	server := newSequenceServer(t, []string{swEnum, swPull, resp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	jobRef, err := client.DestroySwitch(context.Background(), "External")
	if err != nil {
		t.Fatalf("DestroySwitch: %v", err)
	}
	if jobRef == "" {
		t.Error("jobRef should not be empty")
	}

	invokeBody := bodies[2]
	if !strings.Contains(invokeBody, "DestroySystem") {
		t.Errorf("body should call DestroySystem")
	}
	// AffectedSystem に External の Name (GUID) が含まれること
	if !strings.Contains(invokeBody, "AAAAAAAA-1111-1111-1111-AAAAAAAAAAAA") {
		t.Errorf("body should reference target switch GUID")
	}
}

// TestClient_DestroySwitch_Empty
func TestClient_DestroySwitch_Empty(t *testing.T) {
	client, _ := NewClient("http://localhost")
	if _, err := client.DestroySwitch(context.Background(), ""); err == nil {
		t.Error("expected error for empty switchName")
	}
}

// TestClient_DestroySwitch_SelectorSet はスイッチ EPR の Selector が
// MOF に存在するキーだけであることを検証する (#134)。
//
// Msvm_VirtualEthernetSwitch は CIM_ComputerSystem 派生で
// SystemCreationClassName / SystemName を持たない。存在しないキーを送っていると
// #114 のような障害の切り分けで毎回疑う対象になる。
//
// SelectorSet 全体を厳密比較する。Contains だと余計な Selector が増えても通る。
func TestClient_DestroySwitch_SelectorSet(t *testing.T) {
	enum := loadGolden(t, "enumerate_response_virtualethernetswitch.xml")
	pull := loadGolden(t, "pull_response_virtualethernetswitch.xml")
	resp := loadGolden(t, "invoke_response_destroy_switch.xml")

	var bodies []string
	server := newSequenceServer(t, []string{enum, pull, resp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	if _, err := client.DestroySwitch(context.Background(), "Internal"); err != nil {
		t.Fatalf("DestroySwitch: %v", err)
	}
	if len(bodies) != 3 {
		t.Fatalf("用意した応答が全て消費されていない: %d リクエスト", len(bodies))
	}

	// SelectorSet 要素を丸ごと比較する。連結部分文字列の Contains だと、
	// 名前昇順で "Name" より後ろに並ぶ Selector (SystemName 等) を足されても通ってしまう。
	//
	// ⚠️ 見たいのは **AffectedSystem の EPR 内**の SelectorSet。body の先頭には
	// ヘッダの SelectorSet (VESMS の CreationClassName、#145) があるので marker で絞る。
	//
	// marker に msvmVirtualEthernetSwitchURI は使えない。これは
	// msvmVirtualEthernetSwitchManagementServiceURI の**接頭辞**なので、
	// ヘッダの ResourceURI に先にマッチしてしまう。EPR の開始要素を marker にする。
	const want = `<w:Selector Name="CreationClassName">Msvm_VirtualEthernetSwitch</w:Selector>` +
		`<w:Selector Name="Name">BBBBBBBB-2222-2222-2222-BBBBBBBBBBBB</w:Selector>`
	got := selectorSetAfter(t, unescapeForAssert(bodies[2]), "<p:AffectedSystem>")
	if got != want {
		t.Errorf("SelectorSet が一致しない\n got:  %s\n want: %s", got, want)
	}
}

// selectorSetInner は body 中の最初の <w:SelectorSet ...>...</w:SelectorSet> の
// 内側をそのまま返す。要素全体を取り出すことで「Selector が増えている」も検出できる。
func selectorSetInner(t *testing.T, body string) string {
	t.Helper()
	return selectorSetAfter(t, body, "")
}

// selectorSetAfter は marker 以降で最初に現れる SelectorSet の内側を返す。
// 1 つの body に複数の EPR がある場合 (AddResourceSettings の HostResource / Parent 等) に
// 目的の EPR を特定するため、marker には ResourceURI を渡す。
func selectorSetAfter(t *testing.T, body, marker string) string {
	t.Helper()
	if marker != "" {
		m := strings.Index(body, marker)
		if m < 0 {
			t.Fatalf("marker %q が body に無い", marker)
		}
		body = body[m:]
	}
	const openTag, closeTag = `<w:SelectorSet`, `</w:SelectorSet>`
	i := strings.Index(body, openTag)
	if i < 0 {
		t.Fatalf("SelectorSet が見つからない: %s", body)
	}
	j := strings.Index(body[i:], ">")
	if j < 0 {
		t.Fatalf("SelectorSet の開始タグが閉じていない: %s", body)
	}
	start := i + j + 1
	k := strings.Index(body[start:], closeTag)
	if k < 0 {
		t.Fatalf("SelectorSet が閉じていない: %s", body)
	}
	return body[start : start+k]
}

// unescapeForAssert は EPR が SOAP パラメータ内で XML エスケープされている場合に
// 元の文字列へ戻す (アサーションを実際に送られた Selector に対して行うため)。
func unescapeForAssert(s string) string {
	// xmlEscape は全て数値文字参照で出す (#173: 実機が名前付き実体参照を受け付けない)。
	// 名前付きも残してあるのは、過去の応答や手書き fixture を食わせても壊れないようにするため。
	r := strings.NewReplacer(
		"&#60;", "<", "&#62;", ">", "&#38;", "&", "&#34;", `"`, "&#39;", "'",
		"&lt;", "<", "&gt;", ">", "&amp;", "&", "&quot;", `"`, "&apos;", "'",
	)
	return r.Replace(s)
}
