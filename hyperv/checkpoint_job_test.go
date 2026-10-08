package hyperv

import (
	"context"
	"strings"
	"testing"
)

// ResolveCreatedCheckpoint の配線を実機記録で固定する (#125)。
//
// 記録は 2026-10-08、使い捨て VM にチェックポイントを 1 つ作り、Job 完了後に
// Msvm_AffectedJobElement を列挙したもの。4 行入っている:
//
//	pull_other_job_1              別の Job / ElementEffects=0
//	pull_other_job_2              別の Job / ElementEffects=0
//	pull_same_job_effect0         **対象 Job** / ElementEffects=0 (AffectedSystem を指す行)
//	pull_created                  対象 Job / ElementEffects=5 (Create) ← これが答え
//
// 🔴 **この並びに意味がある。** 2 つの絞り込みが両方効いていないと落ちる:
//   - ElementEffects を見ないと pull_same_job_effect0 も当たって 2 件になる
//   - AffectingElement を見ないと別の Job の行まで当たる
//   - 答えが**最後**なので「先頭 1 件を返す」実装では通らない
const (
	// recordedCheckpointJobID は記録に入っている対象 Job の InstanceID (伏せ字)。
	recordedCheckpointJobID = "00000000-0000-4000-8000-000000000066"
	// recordedCreatedCheckpointID は その Job が作成したスナップショットの InstanceID。
	recordedCreatedCheckpointID = "Microsoft:00000000-0000-4000-8000-000000000083"
	// recordedOtherJobID は記録に入っている別 Job の InstanceID (ElementEffects=0 のみ)。
	recordedOtherJobID = "00000000-0000-4000-8000-00000000006d"
)

func recordedAffectedJobElementSequence(t *testing.T) []string {
	t.Helper()
	return []string{
		loadGolden(t, "recorded_affectedjobelement_enumerate.xml"),
		loadGolden(t, "recorded_affectedjobelement_pull_other_job_1.xml"),
		loadGolden(t, "recorded_affectedjobelement_pull_other_job_2.xml"),
		loadGolden(t, "recorded_affectedjobelement_pull_same_job_effect0.xml"),
		loadGolden(t, "recorded_affectedjobelement_pull_created.xml"), // EndOfSequence
	}
}

// TestClient_ResolveCreatedCheckpoint_Recorded は対象 Job が作成した要素を引けることを検証する。
func TestClient_ResolveCreatedCheckpoint_Recorded(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, recordedAffectedJobElementSequence(t), &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ResolveCreatedCheckpoint(context.Background(), recordedCheckpointJobID)
	if err != nil {
		t.Fatalf("ResolveCreatedCheckpoint: %v", err)
	}
	if got != recordedCreatedCheckpointID {
		t.Errorf("= %q, want %q", got, recordedCreatedCheckpointID)
	}
}

// TestClient_ResolveCreatedCheckpoint_OtherJobHasNoCreatedElement は
// ElementEffects=0 しか持たない Job では「見つからない」で返ることを検証する。
//
// 同じ記録を使い **Job だけ変える**。Job 側の絞り込みが効いていないと、
// 対象 Job の Create 行を拾ってしまい成功してしまう。
func TestClient_ResolveCreatedCheckpoint_OtherJobHasNoCreatedElement(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, recordedAffectedJobElementSequence(t), &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ResolveCreatedCheckpoint(context.Background(), recordedOtherJobID)
	if err == nil {
		t.Fatalf("別の Job で成功した (%q)。AffectingElement の絞り込みが効いていない", got)
	}
	if !strings.Contains(err.Error(), "見つからない") {
		t.Errorf("エラーが想定と違う: %v", err)
	}
}

// TestClient_ResolveCreatedCheckpoint_RejectsEmptyJobRef は空の jobRef を弾くことを検証する。
//
// 空だと記録上の全行の AffectingElement と不一致になり「見つからない」で返ってしまい、
// 呼び出し側は「まだ Job が終わっていない」と誤読する。入口で弾く必要がある。
func TestClient_ResolveCreatedCheckpoint_RejectsEmptyJobRef(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, recordedAffectedJobElementSequence(t), &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.ResolveCreatedCheckpoint(context.Background(), "")
	if err == nil {
		t.Fatal("空の jobRef でエラーにならない")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("エラーが想定と違う: %v", err)
	}
	if len(bodies) != 0 {
		t.Errorf("空の jobRef で %d 回通信した。入口で弾くべき", len(bodies))
	}
}

// TestHasElementEffect は実機記録の 4 行の ElementEffects の内訳を固定する。
//
// 実機の記録は 1 行 1 値なので、これだけでは配列として扱う必要性は示せない。
// そこは TestClient_ResolveCreatedCheckpoint_MultiEffectRow (合成) が担う。
func TestHasElementEffect(t *testing.T) {
	// 実機記録から 1 インスタンス取り出して使う (手組みの Instance を作れないため)。
	var bodies []string
	server := newSequenceServer(t, recordedAffectedJobElementSequence(t), &bodies)
	defer server.Close()
	client, _ := NewClient(server.URL)

	instances, err := client.wsman.Enumerate(context.Background(), msvmAffectedJobElementURI)
	if err != nil {
		t.Fatalf("Enumerate: %v", err)
	}
	if len(instances) != 4 {
		t.Fatalf("記録のインスタンス数が %d (want 4)", len(instances))
	}

	var created, plain int
	for _, inst := range instances {
		switch {
		case hasElementEffect(inst, elementEffectsCreate):
			created++
		case hasElementEffect(inst, "0"):
			plain++
		default:
			t.Errorf("ElementEffects が 0 でも 5 でもない行がある: %v", inst.PropertiesList()["ElementEffects"])
		}
	}
	if created != 1 || plain != 3 {
		t.Errorf("Create=%d (want 1) / 0=%d (want 3)", created, plain)
	}
}

// TestClient_ResolveCreatedCheckpoint_MultiEffectRow は ElementEffects が多値の行でも
// Create を拾えることを検証する。
//
// ⚠️ 実機で多値の行は観測していない。合成 fixture
// (synthetic/affectedjobelement_pull_multi_effect.xml) で、
// hasElementEffect を Property (最後の値だけ) に書き換えると落ちることを固定する。
// ElementEffects は MOF 上 uint16[] なので、配列として扱うのが正。
func TestClient_ResolveCreatedCheckpoint_MultiEffectRow(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, []string{
		loadGolden(t, "recorded_affectedjobelement_enumerate.xml"),
		loadGolden(t, "recorded_affectedjobelement_pull_other_job_1.xml"),
		loadGolden(t, "synthetic/affectedjobelement_pull_multi_effect.xml"), // EndOfSequence
	}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ResolveCreatedCheckpoint(context.Background(), recordedCheckpointJobID)
	if err != nil {
		t.Fatalf("ResolveCreatedCheckpoint: %v (ElementEffects を配列として見ていない)", err)
	}
	if got != recordedCreatedCheckpointID {
		t.Errorf("= %q, want %q", got, recordedCreatedCheckpointID)
	}
}

// TestClient_ResolveCreatedCheckpoint_RejectsMultipleCreated は Create 行が複数ある時に
// **黙って先頭を採らない**ことを検証する。
//
// ⚠️ 実機で同一 Job に Create 行が 2 つ出るのは観測していない。
// 「1 件であること」を確かめずに先頭を返す実装を落とすための合成
// (synthetic/affectedjobelement_pull_created_no_eos.xml)。
func TestClient_ResolveCreatedCheckpoint_RejectsMultipleCreated(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, []string{
		loadGolden(t, "recorded_affectedjobelement_enumerate.xml"),
		loadGolden(t, "synthetic/affectedjobelement_pull_created_no_eos.xml"),
		loadGolden(t, "recorded_affectedjobelement_pull_created.xml"), // EndOfSequence
	}, &bodies)
	defer server.Close()

	client, _ := NewClient(server.URL)
	got, err := client.ResolveCreatedCheckpoint(context.Background(), recordedCheckpointJobID)
	if err == nil {
		t.Fatalf("Create 行が 2 つあるのに成功した (%q)。1 件に絞れたか確かめていない", got)
	}
	if !strings.Contains(err.Error(), "2 件ある") {
		t.Errorf("エラーが想定と違う (件数を報告していない): %v", err)
	}
}
