package hyperv

import "github.com/google/uuid"

// IsHostComputerSystem は この Msvm_ComputerSystem が Hyper-V ホスト自身 (管理 OS) かを返す。
//
// Msvm_ComputerSystem は VM とホストの両方を表すため、VM 一覧を取るときは除外が要る (#139)。
//
// 判定に Name を使う理由: MOF は Name を "always set to GUID" と書いているが、**実機のホスト
// エントリは GUID ではなくコンピューター名を返す** (2026-09-27 実測: ホスト Name="DESKTOP-..."、
// VM Name="A1B2C3D4-1111-2222-3333-444455556666")。VM 側は必ず GUID なので、GUID として
// 解釈できるかどうかで切れる。以降のコメント中のホスト名・GUID は匿名化した例示値で、
// 実機の値そのものではない (本リポジトリは公開のため)。
//
// Caption / Description を使わないのは、これらが**ロケールで変わる**ため (クラスに AMENDMENT
// 修飾子が付く)。実機 (日本語ホスト) では Caption がホスト「ホスト コンピューター システム」/
// VM「仮想マシン」、Description がホスト「Microsoft ホスト コンピューター システム」/
// VM "Microsoft Virtual Machine" と返る。**Description はホストだけ日本語で VM は英語**という
// 非対称があり、VM だけ見て検証すると「英語だから安全」と誤判断する。#98 と同型の罠。
//
// もう 1 つのロケール非依存な材料として InstallDate がある (MOF: VM は構成の作成日時、
// 管理 OS は Null。実機でもホストだけ xsi:nil)。こちらを併用しないのは、既存の合成 golden が
// InstallDate を持たず、追加するには fixture の手書きが要るため (本リポジトリは記録器由来の
// fixture のみを許す)。実機の記録が入ったら AND 条件に締めてよい。
func (cs *Msvm_ComputerSystem) IsHostComputerSystem() bool {
	_, err := uuid.Parse(cs.Name)
	return err != nil
}

// filterHostComputerSystems は列挙結果から Hyper-V ホスト自身だけを残す。
func filterHostComputerSystems(all []*Msvm_ComputerSystem) []*Msvm_ComputerSystem {
	out := make([]*Msvm_ComputerSystem, 0, 1)
	for _, cs := range all {
		if cs.IsHostComputerSystem() {
			out = append(out, cs)
		}
	}
	return out
}

// filterOutHostComputerSystems は列挙結果から Hyper-V ホスト自身を取り除く。
//
// ListComputerSystems から切り出してあるのは、この絞り込みを struct だけで
// テストできるようにするため。実機の Enumerate 応答を golden にできない間
// (記録器が実機に到達できない)、配線そのものは固定できないが、落とす条件は固定できる。
func filterOutHostComputerSystems(all []*Msvm_ComputerSystem) []*Msvm_ComputerSystem {
	out := make([]*Msvm_ComputerSystem, 0, len(all))
	for _, cs := range all {
		if cs.IsHostComputerSystem() {
			continue
		}
		out = append(out, cs)
	}
	return out
}
