package hyperv

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

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
// もう 1 つのロケール非依存な材料として InstallDate がある。MOF は
// "The date and time the virtual machine configuration was created for a virtual machine,
// or Null, for a management operating system." と書いており、実機記録でもホストだけ
// xsi:nil で VM 4 台は値を持つ。材料の一覧と根拠は testdata/mof/
// msvm_computersystem_host_signals.txt に写してある。
//
// # なぜ Name 単独のままにしているか (#185)
//
// 2 つの材料を AND にしても**実機 1 台の観測で挙動を変えることになる**。
// 締める前に「2 つが食い違う実機があるか」を知りたいが、それには締めずに観測する必要がある。
// そこで判定は変えず、observeHostSignalDisagreement で食い違いを記録から検出している。
//
// ⚠️ **#185 本文のリスク分析は誤りだった。** 「AND にすると InstallDate を返さない実機で
// 全 VM がホスト扱いになり ListComputerSystems が 0 件を返す」と書いてあったが、
// AND では VM は落ちない。VM の Name は GUID なので第 1 条件 (GUID として解釈できない)
// を満たさず、InstallDate が何であれホスト扱いにならない。
//
//	AND にしたときの失敗 : ホストの InstallDate が非 Null → ホストが落ちず一覧に混じる
//	                      + hostComputerSystem が 0 件でエラー
//	VM が落ちる失敗      : OR にした場合。InstallDate が Null の VM をホストと見て
//	                      ListComputerSystems の結果から除く
//
// つまり VM が消える方向は OR で、AND ではない。
//
// 🔴 **ただし「だから provider が壊れる」とは言えない。** この判定を通るのは
// ListComputerSystems と hostComputerSystem だけで、
//
//	ListComputerSystems  : 非テストの呼び出し元が無い (provider も統合テストでしか使わない)
//	hostComputerSystem   : switch.go の CreateSwitch (Internal) 1 箇所のみ
//
// provider の VM の存在確認と Read は **FindComputerSystemByElementName** を使う
// (25 箇所)。こちらは enumerateFiltered で ElementName 一致を取るだけで
// **この判定を通らない**。#185 本文は「provider の Read が ListComputerSystems を使う」
// 前提で書かれていたが、それは成り立たない。
//
// ⚠️ 逆に、FindComputerSystemByElementName は**ホストのインスタンスも検索対象に含む**。
// MOF はホストの ElementName を「管理 OS の NetBIOS 名」と書いているので、
// VM の表示名がそれと一致すると `2 VMs found; name is ambiguous` になる。
// 「ホストが結果に混じる」懸念は本判定とは別経路で既に存在する (本 PR の範囲外)。
//
// # 第 3 の材料はあるが使えない
//
// OnTimeInMilliseconds も MOF 上は管理 OS で Null だが、**struct が uint64 なので
// Null が 0 に潰れ、停止中の VM と区別できない** (実機記録の pull_vm_off は 0)。
// 使うならポインタ型か「nil だったか」を保持する必要がある。
func (cs *Msvm_ComputerSystem) IsHostComputerSystem() bool {
	_, err := uuid.Parse(cs.Name)
	return err != nil
}

// installDateSuggestsHost は InstallDate を材料にした「ホストらしさ」を返す。
//
// 🔴 **判定には使っていない (#185)。** Name 単独の判定と食い違わないかを観測し、
// Name 規則が崩れたときの診断に添えるためだけのもの。ここを
// IsHostComputerSystem から呼ぶと、実機 1 台の観測で挙動を変えることになる。
//
// MOF は管理 OS では Null と書いている。struct は string なので Null は "" で届く。
func (cs *Msvm_ComputerSystem) installDateSuggestsHost() bool {
	return strings.TrimSpace(cs.InstallDate) == ""
}

// observeHostSignalDisagreement は Name 由来の判定と InstallDate 由来の判定が
// 食い違うインスタンスを返す (#185)。
//
// 判定を変えずに観測を貯めるための入口。実機記録を足したときにテストが拾う。
func observeHostSignalDisagreement(all []*Msvm_ComputerSystem) []*Msvm_ComputerSystem {
	var out []*Msvm_ComputerSystem
	for _, cs := range all {
		if cs.IsHostComputerSystem() != cs.installDateSuggestsHost() {
			out = append(out, cs)
		}
	}
	return out
}

// hostSignalHint は Name 規則が崩れたときのエラーに添える、第 2 の材料の内訳 (#185)。
//
// 「ホストが見つからない」「候補が複数ある」と言われても、Name 規則が壊れたのか
// 応答自体がおかしいのか分からない。InstallDate 側が何件をホストと見るかを併記すると、
// 第 2 の材料がまだ機能しているかがその場で分かる。
func hostSignalHint(all []*Msvm_ComputerSystem) string {
	byInstallDate := 0
	for _, cs := range all {
		if cs.installDateSuggestsHost() {
			byInstallDate++
		}
	}
	return fmt.Sprintf(" (全 %d 件のうち InstallDate が空なのは %d 件。"+
		"InstallDate 側が 1 件なら Name 規則が崩れている)", len(all), byInstallDate)
}

// pickHostComputerSystem は列挙結果からホスト自身を 1 件だけ取り出す。
//
// 不在・複数一致はエラーにする。IsHostComputerSystem の判定 (Name が GUID として
// 解釈できないか) が将来崩れたときに、黙って別のインスタンスを掴むより落ちて気付く方を選ぶ。
//
// Client から切り出してあるのは、この分岐を struct だけでテストできるようにするため。
// 配線は実機記録で固定済 (#167、host_exclusion_wiring_test.go)。
func pickHostComputerSystem(all []*Msvm_ComputerSystem) (*Msvm_ComputerSystem, error) {
	hosts := filterHostComputerSystems(all)
	switch len(hosts) {
	case 1:
		return hosts[0], nil
	case 0:
		return nil, fmt.Errorf("pickHostComputerSystem: ホストの Msvm_ComputerSystem が見つからない%s",
			hostSignalHint(all))
	default:
		return nil, fmt.Errorf("pickHostComputerSystem: ホスト候補が %d 件あり一意に決まらない%s",
			len(hosts), hostSignalHint(all))
	}
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
// テストできるようにするため。
//
// **配線 (ListComputerSystems が実際に呼んでいるか) は実機記録で固定済** (#167)。
// host_exclusion_wiring_test.go を参照。
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
