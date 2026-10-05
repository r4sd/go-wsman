package hyperv

import (
	"context"
	"testing"
)

// ホスト除外の **配線** を実機記録で固定する (#167)。
//
// 🔴 以前は「落とす条件」(`IsHostComputerSystem` / `filterOutHostComputerSystems`) だけを
// struct テストで固定しており、**`ListComputerSystems` に実際に配線されているか**は
// 何も守っていなかった。`filterOutHostComputerSystems` の呼び出しを消しても緑だった。
//
// 配線を固定するには**ホストのインスタンスを含む Enumerate 応答**が要る。
// 手書き golden は関所が弾き、合成も実機記録を派生元に要求するため、
// **実機から記録する**のが唯一の正道だった (本 Issue の「やること 1・2」)。
//
// 2026-10-05、`lanrelay.py` (homelab-infra) 経由で `go test -tags=integration` が
// 実機に届くようになり記録できた。伏せ字で ホスト名 / VM 名 / GUID / IP は置換済。
//
// 記録の内容 (実機の Hyper-V ホスト、VM 3 台):
//
//	recorded_computersystem_enumerate.xml  EnumerateResponse
//	recorded_computersystem_pull_2.xml     ホスト (Name が GUID ではない)
//	recorded_computersystem_pull_3,4,5.xml VM 3 台 (Name は GUID、5 に EndOfSequence)
//
// recordedHostName は実機記録でホストの Name / ElementName に入っている伏せ字。
//
// ⚠️ **番号は「伏せ字リストの長さ順位」で決まる** (wsman/record.go の scrub は
// literals を長さ降順に走査する)。ホストが 1 なのは今の環境でホスト名が最長だったから。
// 記録を採り直したときに長い VM 名 / スイッチ名があると番号が変わる。
// そのときは switch_test.go の期待値も一緒に直す (定数 1 箇所で済むようにしてある)。
const recordedHostName = "scrubbed-1"

// 🔴 **ホストを先頭に置かない。** 先頭だと「先頭 1 件を返す」だけの実装でも
// pickHostComputerSystem のテストが通ってしまう (手書き golden では意図して末尾に
// 置いていた性質。記録に置き換えた際に一度失って変異が生き残った)。
// 各 Pull 応答は独立しているので、EndOfSequence を持つ pull_5 だけ末尾に固定して
// 残りは並べ替えてよい。
func recordedComputerSystemSequence(t *testing.T) []string {
	t.Helper()
	return []string{
		loadGolden(t, "recorded_computersystem_enumerate.xml"),
		loadGolden(t, "recorded_computersystem_pull_3.xml"), // VM
		loadGolden(t, "recorded_computersystem_pull_4.xml"), // VM
		loadGolden(t, "recorded_computersystem_pull_2.xml"), // ホスト (先頭に置かない)
		loadGolden(t, "recorded_computersystem_pull_5.xml"), // VM + EndOfSequence
	}
}

// recordedVMNames は記録に入っている VM 3 台の Name (GUID プレースホルダ)。
//
// 件数と「ホストでないこと」だけ見ると、**別のインスタンスを落として別のを残す**型の
// 誤りが通る。残った 3 件が期待どおりであることも固定する。
var recordedVMNames = []string{
	"00000000-0000-4000-8000-00000000000a",
	"00000000-0000-4000-8000-00000000000e",
	"00000000-0000-4000-8000-000000000011",
}

// TestClient_ListComputerSystems_ExcludesHost_Recorded は実機記録に対して
// ListComputerSystems がホストを落とすことを検証する。
func TestClient_ListComputerSystems_ExcludesHost_Recorded(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, recordedComputerSystemSequence(t), &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ListComputerSystems(context.Background())
	if err != nil {
		t.Fatalf("ListComputerSystems: %v", err)
	}

	// 記録には 4 インスタンス (ホスト 1 + VM 3) 入っている。
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (ホストが落ちる)。実機記録にはホスト 1 + VM 3 が入っている", len(got))
	}
	for _, cs := range got {
		if cs.IsHostComputerSystem() {
			t.Errorf("ホストが残っている: Name=%q ElementName=%q", cs.Name, cs.ElementName)
		}
	}
	// 残った 3 件が期待どおりであること (別のインスタンスを落としていないか)。
	gotNames := make(map[string]bool, len(got))
	for _, cs := range got {
		gotNames[cs.Name] = true
	}
	for _, want := range recordedVMNames {
		if !gotNames[want] {
			t.Errorf("VM %s が結果に無い (別のインスタンスを落としている)", want)
		}
	}
}

// TestClient_listComputerSystemsIncludingHost_Recorded はホストを**落とさない**ことを検証する。
//
// #157 の伏せ字収集 (discoverScrubNames) はホストの ElementName を必要とするため、
// こちらが落としてしまうと実ホスト名が公開 golden に漏れる (#166 で判明した依存)。
func TestClient_listComputerSystemsIncludingHost_Recorded(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, recordedComputerSystemSequence(t), &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	all, err := client.listComputerSystemsIncludingHost(context.Background())
	if err != nil {
		t.Fatalf("listComputerSystemsIncludingHost: %v", err)
	}

	if len(all) != 4 {
		t.Fatalf("len = %d, want 4 (ホスト 1 + VM 3。ホストを落としてはいけない)", len(all))
	}
	hosts := filterHostComputerSystems(all)
	if len(hosts) != 1 {
		t.Fatalf("ホストが %d 件 (want 1)", len(hosts))
	}
	// ホストの ElementName が取れること (discoverScrubNames がこれを使う)。
	// 有無ではなく値で見る (空でなければ通る形だと別のインスタンスでも緑になる)。
	if hosts[0].ElementName != recordedHostName {
		t.Errorf("ホストの ElementName が %q (want %q)。伏せ字収集がホスト名を拾えない",
			hosts[0].ElementName, recordedHostName)
	}
}

// TestClient_hostComputerSystem_Recorded は実機記録からホストを一意に引けることを検証する。
func TestClient_hostComputerSystem_Recorded(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, recordedComputerSystemSequence(t), &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	host, err := client.hostComputerSystem(context.Background())
	if err != nil {
		t.Fatalf("hostComputerSystem: %v", err)
	}
	if !host.IsHostComputerSystem() {
		t.Errorf("ホストではないインスタンスを掴んだ: Name=%q", host.Name)
	}
	// 値で固定する。IsHostComputerSystem だけ見ると、判定が壊れたときに
	// 「ホストだと誤認した別のインスタンス」で通ってしまう。
	if host.Name != recordedHostName {
		t.Errorf("Name が %q (want %q)", host.Name, recordedHostName)
	}
}
