package hyperv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestClient_GetVirtualHardDisk は VHD ファイルパスから設定情報を取得するテスト。
func TestClient_GetVirtualHardDisk(t *testing.T) {
	respXML := loadGolden(t, "invoke_response_get_vhd.xml")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Errorf("read body: %v", err)
		}
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(respXML))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := client.GetVirtualHardDisk(context.Background(), `D:\VMs\test.vhdx`)
	if err != nil {
		t.Fatalf("GetVirtualHardDisk: %v", err)
	}

	if got.Path != `D:\VMs\test.vhdx` {
		t.Errorf("Path: got %q", got.Path)
	}
	if got.VirtualDiskFormat != VHDFormatVHDX {
		t.Errorf("VirtualDiskFormat: got %d", got.VirtualDiskFormat)
	}
	if got.VirtualDiskType != VHDTypeDynamic {
		t.Errorf("VirtualDiskType: got %d", got.VirtualDiskType)
	}
	if got.MaxInternalSize != 10737418240 {
		t.Errorf("MaxInternalSize: got %d", got.MaxInternalSize)
	}
}

// TestClient_CreateVirtualHardDisk は VHD 作成リクエストが正しく組み立てられ、非同期 Job
// (Msvm_StorageJob) の完了を内部で待つことを検証する。
//
// 想定リクエスト: 1) CreateVirtualHardDisk invoke (4096 + Job EPR)、2) Job の Get (完了)。
// WaitForJobEPR が Job EPR の ResourceURI (Msvm_StorageJob) を使って Get することも検証する。
func TestClient_CreateVirtualHardDisk(t *testing.T) {
	invokeResp := loadGolden(t, "invoke_response_create_vhd.xml")
	// 🔴 **Msvm_StorageJob の実機記録を使う (#165)。** 以前は Msvm_ConcreteJob の
	// golden を流用していた。パーサは depth-2 のローカル名しか見ないので構造的には
	// 同等に扱えるが、「StorageJob の実応答でも読める」証拠がリポジトリに無かった。
	jobResp := loadGolden(t, storageJobCompletedGolden)

	var bodies []string
	server := newSequenceServer(t, []string{invokeResp, jobResp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	settings := Msvm_VirtualHardDiskSettingData{
		VirtualDiskFormat: VHDFormatVHDX,
		VirtualDiskType:   VHDTypeDynamic,
		Path:              `D:\VMs\new.vhdx`,
		MaxInternalSize:   10737418240,
	}

	jobRef, err := client.CreateVirtualHardDisk(context.Background(), &settings)
	if err != nil {
		t.Fatalf("CreateVirtualHardDisk: %v", err)
	}
	// 内部で Job 完了まで待つため、戻りの Job 参照は空。
	if jobRef != "" {
		t.Errorf("待機済みなので jobRef は空のはず, got %q", jobRef)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 requests (invoke + job get), got %d", len(bodies))
	}

	// 1 番目 (invoke) に VirtualDiskSettingData / Path / メソッド名が含まれること。
	if !strings.Contains(bodies[0], "VirtualDiskSettingData") || !strings.Contains(bodies[0], `D:\VMs\new.vhdx`) ||
		!strings.Contains(bodies[0], "CreateVirtualHardDisk") {
		t.Errorf("invoke body に必要な要素が無い")
	}
	// 2 番目 (Job Get) が Msvm_StorageJob URI を使っていること (ConcreteJob ではない)。
	if !strings.Contains(bodies[1], "Msvm_StorageJob") {
		t.Errorf("Job Get は Msvm_StorageJob URI を使うべき; body: %s", bodies[1])
	}
}

// TestClient_ResizeVirtualHardDisk は Resize リクエストの組み立てと Job (StorageJob) 待機を検証する。
func TestClient_ResizeVirtualHardDisk(t *testing.T) {
	invokeResp := loadGolden(t, "invoke_response_resize_vhd.xml")
	// 🔴 **Msvm_StorageJob の実機記録を使う (#165)。** 以前は Msvm_ConcreteJob の
	// golden を流用していた。パーサは depth-2 のローカル名しか見ないので構造的には
	// 同等に扱えるが、「StorageJob の実応答でも読める」証拠がリポジトリに無かった。
	jobResp := loadGolden(t, storageJobCompletedGolden)

	var bodies []string
	server := newSequenceServer(t, []string{invokeResp, jobResp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	const (
		path    = `D:\VMs\resize.vhdx`
		newSize = uint64(21474836480) // 20 GiB
	)

	jobRef, err := client.ResizeVirtualHardDisk(context.Background(), path, newSize)
	if err != nil {
		t.Fatalf("ResizeVirtualHardDisk: %v", err)
	}
	if jobRef != "" {
		t.Errorf("待機済みなので jobRef は空のはず, got %q", jobRef)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(bodies))
	}
	if !strings.Contains(bodies[0], "ResizeVirtualHardDisk") || !strings.Contains(bodies[0], path) ||
		!strings.Contains(bodies[0], "21474836480") {
		t.Errorf("invoke body に必要な要素が無い")
	}
	if !strings.Contains(bodies[1], "Msvm_StorageJob") {
		t.Errorf("Job Get は Msvm_StorageJob URI を使うべき; body: %s", bodies[1])
	}
}

// TestClient_CreateVirtualHardDisk_JobGone は Job の Get が DestinationUnreachable (不在) を返したら
// エラーになることを検証する (旧 isJobGoneFault ハックの回帰。消えた Job を成功偽装しない)。
func TestClient_CreateVirtualHardDisk_JobGone(t *testing.T) {
	invokeResp := loadGolden(t, "invoke_response_create_vhd.xml")
	faultResp := loadGolden(t, "fault_destination_unreachable.xml")

	var bodies []string
	server := newSequenceServer(t, []string{invokeResp, faultResp}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	settings := Msvm_VirtualHardDiskSettingData{
		VirtualDiskFormat: VHDFormatVHDX, VirtualDiskType: VHDTypeDynamic,
		Path: `D:\VMs\new.vhdx`, MaxInternalSize: 1 << 30,
	}
	if _, err := client.CreateVirtualHardDisk(context.Background(), &settings); err == nil {
		t.Fatal("Job が見つからない場合はエラーになるべき (成功偽装しない)")
	}
}

// TestClient_ResizeVirtualHardDisk_EmptyPath は Path 未指定時に
// パラメータ検証エラーになることを確認する。
func TestClient_ResizeVirtualHardDisk_EmptyPath(t *testing.T) {
	client, err := NewClient("http://example.invalid")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.ResizeVirtualHardDisk(context.Background(), "", 1024)
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
	if !strings.Contains(err.Error(), "path") {
		t.Errorf("error should mention path, got: %v", err)
	}
}

// TestStorageJobGoldensAreRealStorageJobResponses は VHD 系の Job 待機に使う golden が
// **実機の Msvm_StorageJob 応答**であることを固定する (#165)。
//
// 🔴 以前は Msvm_ConcreteJob の golden を流用していた。パーサは depth-2 のローカル名しか
// 見ないので構造的には同等に扱え、**struct には両者を区別するフィールドが無い**
// (Msvm_ConcreteJob は InstanceID / JobState / PercentComplete / ErrorCode / ErrorDescription だけ)。
// つまり振る舞いでは区別できないので、**応答そのものの中身**で固定する。
//
// MOF 突合は「名前と型が合っている」しか言えず、wire format が合っているかは別問題だった。
// 実機記録が入ったので、以降の変更は実応答に対して検証される。
// --- VHD 系の Job 待機に使う golden ---

// storageJobCompletedGolden / storageJobRunningGolden は VHD 系の Job 待機に使う golden。
// **名前をここ 1 箇所に集約する。**
//
// 定数を差し替えれば下の provenance テストが落ちる。ただし**各テストが直接
// ファイル名を書く経路は定数では防げない**(実際にその変異は生き残る)。
// そこを塞ぐのが TestVhdTestsDoNotReuseConcreteJobGolden。
const (
	storageJobCompletedGolden = "get_response_storagejob_completed.xml"
	storageJobRunningGolden   = "get_response_storagejob_running.xml"
)

func TestStorageJobGoldensAreRealStorageJobResponses(t *testing.T) {
	for _, tt := range []struct {
		file            string
		wantState       string
		wantParsedState uint16
	}{
		{storageJobCompletedGolden, "7", JobStateCompleted},
		{storageJobRunningGolden, "4", JobStateRunning},
	} {
		t.Run(tt.file, func(t *testing.T) {
			resp := loadGolden(t, tt.file)
			// ルート要素が StorageJob であること。ConcreteJob の golden に戻すと落ちる。
			if !strings.Contains(resp, "Msvm_StorageJob") {
				t.Error("Msvm_StorageJob の応答ではない。" +
					"Msvm_ConcreteJob の golden を流用していないか (#165)")
			}
			if strings.Contains(resp, "Msvm_ConcreteJob") {
				t.Error("Msvm_ConcreteJob が混ざっている")
			}
			// 記録器の印があること (手書きに差し替えられていないか)。
			if !strings.Contains(resp, "recorded-by") {
				t.Error("記録器の印が無い。手書きの golden に差し替えられていないか")
			}
			if !strings.Contains(resp, "<p:JobState>"+tt.wantState+"</p:JobState>") {
				t.Errorf("JobState が %s でない", tt.wantState)
			}
			// **パーサが実応答から読めること。** 中身の検査だけだと
			// 「ファイルはあるが読めていない」が通る。
			var bodies []string
			server := newSequenceServer(t, []string{resp}, &bodies)
			defer server.Close()
			client, _ := NewClient(server.URL)
			job, err := client.getJob(context.Background(), nsVirtV2+"/Msvm_StorageJob", "dummy")
			if err != nil {
				t.Fatalf("getJob: %v", err)
			}
			if job.ErrorCode != 0 {
				t.Errorf("ErrorCode = %d, want 0", job.ErrorCode)
			}
			if job.JobState != tt.wantParsedState {
				t.Errorf("JobState = %d, want %d", job.JobState, tt.wantParsedState)
			}
		})
	}
}

// TestClient_CreateVirtualHardDisk_StorageJobPolling は Job が実行中の間ポーリングし、
// 完了で返ることを実機記録で検証する (#165)。
//
// 使う応答はどちらも実機記録 (実行中 JobState=4 → 完了 JobState=7)。
func TestClient_CreateVirtualHardDisk_StorageJobPolling(t *testing.T) {
	invokeResp := loadGolden(t, "invoke_response_create_vhd.xml")
	running := loadGolden(t, storageJobRunningGolden)
	completed := loadGolden(t, storageJobCompletedGolden)

	var bodies []string
	server := newSequenceServer(t, []string{invokeResp, running, completed}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	// 戻り値の Job 参照は**常に空**。待機を内部で済ませてから返る契約
	// (doc に明記されている)。完了を待ったことはリクエスト数で確かめる。
	jobRef, err := client.CreateVirtualHardDisk(context.Background(), &Msvm_VirtualHardDiskSettingData{
		Path:              `D:\VMs\test.vhdx`,
		VirtualDiskFormat: VHDFormatVHDX,
		VirtualDiskType:   VHDTypeDynamic,
		MaxInternalSize:   1 << 30,
	}, WithPollInterval(time.Millisecond))
	if err != nil {
		t.Fatalf("CreateVirtualHardDisk: %v", err)
	}
	if jobRef != "" {
		t.Errorf("JobRef が %q (want 空)。待機済みなので返さない契約", jobRef)
	}

	// invoke 1 回 + Job の Get 2 回 = 3。実行中の応答を飛ばすと 2 になる。
	if len(bodies) != 3 {
		t.Fatalf("リクエストが %d 件 (want 3 = invoke 1 + Job の Get 2)。"+
			"実行中 (JobState=4) をポーリングしているか", len(bodies))
	}
}

// TestVhdTestsDoNotReuseConcreteJobGolden は vhd_test.go が Msvm_ConcreteJob の golden を
// 参照していないことをソースレベルで検証する (#165)。
//
// 🔴 定数への集約だけでは**各テストが直接ファイル名を書く経路**を防げない。
// 批判的レビューで実際にその変異が生き残った (定数を迂回して流用に戻せる)。
//
// 「ConcreteJob の golden を VHD 系の Job 待機に流用しない」が守りたい性質なので、
// 振る舞いではなくソースで直接禁じる (internal/guard の TestNoRawXMLInSources と同型)。
func TestVhdTestsDoNotReuseConcreteJobGolden(t *testing.T) {
	src, err := os.ReadFile("vhd_test.go")
	if err != nil {
		t.Fatalf("vhd_test.go を読めない: %v", err)
	}
	// 🔴 **接頭辞で数える。** 以前は `..._completed.xml` だけを数えていたが、
	// testdata には `_running.xml` / `_exception.xml` もあり、
	// **running を流用に戻す変異が生き残った** (Fable のレビューで実証)。
	// ポーリングテスト自身はリクエスト数しか見ていないので、
	// ConcreteJob の running 応答でも緑になる。
	//
	// ⚠️ **この接頭辞自身が vhd_test.go に現れる**ので、出現回数で見る。
	// ちょうど 1 回 (= ここ) を期待し、
	//   2 回以上 → どこかのテストが流用に戻した
	//   0 回     → 接頭辞が変わってこの検査が空振りになった
	// のどちらも検出する。
	//
	// 観測範囲: **vhd_test.go 内のリテラルのみ**。接頭辞を 2 つの文字列の連結で
	// 書いた場合や、VHD のテストを別ファイルへ移した場合は検出できない。
	// ソース grep の限界として受け入れる (本当に塞ぐなら golden の読み込みを
	// 1 箇所に通して実行時に見るしかない)。
	//
	// ⚠️ このコメントに接頭辞そのものを書くと検査が自分で落ちる (一度やった)。
	const reused = "get_response_concretejob_"
	switch n := strings.Count(string(src), reused); {
	case n > 1:
		t.Errorf("vhd_test.go が %s* を %d 箇所で参照している (期待 1 = この検査自身)。\n"+
			"  VHD 系の Job は Msvm_StorageJob なので、実機記録 (%s / %s) を使うこと (#165)。\n"+
			"  MOF 突合は「名前と型が合っている」しか言えず、wire format は別問題。",
			reused, n, storageJobCompletedGolden, storageJobRunningGolden)
	case n == 0:
		t.Errorf("%s* が vhd_test.go に 1 箇所も無い。この検査が空振りしている "+
			"(接頭辞が変わったか、検査ごと消えたか)", reused)
	}
}
