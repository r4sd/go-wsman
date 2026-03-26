#!/usr/bin/env node

/**
 * Terraform Plan Summary を分析してWell-Architectedレビューと次のアクションを生成
 *
 * Usage: node plan-analyzer.js summary.json
 */

const fs = require('fs');

if (process.argv.length < 3) {
  console.error('Usage: node plan-analyzer.js <summary.json>');
  process.exit(1);
}

const summaryPath = process.argv[2];
const summary = JSON.parse(fs.readFileSync(summaryPath, 'utf8'));

class PlanAnalyzer {
  constructor(summary) {
    this.summary = summary;
  }

  /**
   * セキュリティ分析
   */
  analyzeSecurity() {
    const { wellArchitected, resourceChanges } = this.summary;
    const current = [];
    const ideal = [];

    if (wellArchitected.hasEncryption) {
      current.push('✅ 暗号化設定あり');
    } else {
      current.push('⚠️ 暗号化設定が見当たらない');
      ideal.push('RDS/S3/EBSで暗号化を有効化すべき');
    }

    if (wellArchitected.securityGroups.length > 0) {
      current.push(`⚠️ Security Group変更あり (${wellArchitected.securityGroups.length}件) - ルール確認必須`);
      ideal.push('Security Groupは最小権限の原則に従う');
    }

    if (wellArchitected.iamPolicies.length > 0) {
      current.push(`⚠️ IAM変更あり (${wellArchitected.iamPolicies.length}件) - 権限範囲を確認`);
      ideal.push('IAMポリシーは必要最小限の権限のみ付与');
    }

    return { current, ideal };
  }

  /**
   * 信頼性分析
   */
  analyzeReliability() {
    const { wellArchitected } = this.summary;
    const current = [];
    const ideal = [];

    const rdsCount = wellArchitected.rdsInstances.filter(
      r => r.action === 'create' || r.action === 'no-op'
    ).length;

    if (wellArchitected.hasMultiAz) {
      current.push('✅ マルチAZ構成あり');
    } else {
      current.push('⚠️ シングルAZ構成の可能性');
      ideal.push('本番環境ではマルチAZ構成にすべき');
    }

    if (wellArchitected.hasBackup) {
      current.push('✅ バックアップ設定あり');
    } else {
      ideal.push('自動バックアップとスナップショット設定を推奨');
    }

    if (rdsCount > 0) {
      current.push(`📊 RDSインスタンス数: ${rdsCount}`);
      if (rdsCount < 2) {
        ideal.push('リードレプリカを追加して可用性向上を推奨');
      } else {
        current.push('✅ リードレプリカ構成で可用性向上');
      }
    }

    return { current, ideal };
  }

  /**
   * パフォーマンス分析
   */
  analyzePerformance() {
    const { wellArchitected } = this.summary;
    const current = [];
    const ideal = [];

    const rdsCount = wellArchitected.rdsInstances.filter(
      r => r.action === 'create' || r.action === 'no-op'
    ).length;

    if (rdsCount > 1) {
      current.push('✅ リードレプリカで読み取り性能向上');
    }

    if (wellArchitected.hasAutoScaling) {
      current.push('✅ Auto Scaling設定あり');
    } else {
      ideal.push('本番環境ではAuto Scalingを設定すべき');
    }

    ideal.push('CloudWatch メトリクスで継続的に監視');

    if (current.length === 0) {
      current.push('変更による直接的な影響なし');
    }

    return { current, ideal };
  }

  /**
   * コスト分析
   */
  analyzeCost() {
    const { costs } = this.summary;
    const items = [];
    let totalChange = 0;

    costs.forEach(cost => {
      const sign = cost.monthly_cost > 0 ? '+' : '';
      items.push({
        resource: `${cost.type.toUpperCase()} (${cost.resource})`,
        action: cost.action,
        impact: `${sign}$${cost.monthly_cost}/月`
      });
      totalChange += cost.monthly_cost;
    });

    return {
      items,
      totalChange,
      savings: [
        { name: 'Aurora Serverless v2に変更', amount: -60 },
        { name: '開発環境を夜間停止', amount: -80 },
        { name: 'NAT Gateway削除（VPC Endpoints利用）', amount: -35 }
      ]
    };
  }

  /**
   * 運用性分析
   */
  analyzeOperability() {
    const { wellArchitected } = this.summary;
    const current = [];
    const ideal = [];

    if (wellArchitected.hasMonitoring) {
      current.push('✅ モニタリング設定あり');
    } else {
      current.push('⚠️ Container Insights未設定');
      ideal.push('ECS Container Insightsでメトリクス可視化すべき');
    }

    if (wellArchitected.hasLogging) {
      current.push('✅ ログ設定あり');
    } else {
      ideal.push('ALBアクセスログをS3に保存すべき');
    }

    ideal.push('CloudWatch Dashboardで一元管理');

    return { current, ideal };
  }

  /**
   * 次のアクション提案を生成（小さいPRを意識）
   */
  generateNextActions() {
    const { wellArchitected, stats } = this.summary;
    const actions = [];

    // セキュリティ関連の次のアクション
    if (!wellArchitected.hasEncryption) {
      actions.push({
        priority: '高',
        category: 'セキュリティ',
        title: 'RDS/S3暗号化を有効化',
        description: '次のPRでRDS・S3バケットの暗号化を追加',
        estimatedEffort: '小'
      });
    }

    if (wellArchitected.securityGroups.length > 0) {
      actions.push({
        priority: '高',
        category: 'セキュリティ',
        title: 'Security Groupルールの見直し',
        description: '不要なingressルールを削除し、最小権限の原則を適用',
        estimatedEffort: '小'
      });
    }

    // 信頼性関連の次のアクション
    const rdsCount = wellArchitected.rdsInstances.filter(
      r => r.action === 'create' || r.action === 'no-op'
    ).length;

    if (rdsCount === 1) {
      actions.push({
        priority: '中',
        category: '信頼性',
        title: 'RDSリードレプリカ追加',
        description: 'database_instance_count を 2 に変更（次のPR）',
        estimatedEffort: '小'
      });
    }

    if (!wellArchitected.hasMultiAz) {
      actions.push({
        priority: '中',
        category: '信頼性',
        title: 'マルチAZ構成への変更',
        description: 'multi_az = true に設定（本番環境）',
        estimatedEffort: '小'
      });
    }

    if (!wellArchitected.hasBackup) {
      actions.push({
        priority: '中',
        category: '信頼性',
        title: 'バックアップ設定追加',
        description: 'backup_retention_period を 7 に設定',
        estimatedEffort: '小'
      });
    }

    // 運用性関連の次のアクション
    if (!wellArchitected.hasMonitoring) {
      actions.push({
        priority: '中',
        category: '運用性',
        title: 'Container Insights有効化',
        description: 'enable_container_insights = true に設定（次のPR）',
        estimatedEffort: '小'
      });
    }

    if (!wellArchitected.hasLogging) {
      actions.push({
        priority: '低',
        category: '運用性',
        title: 'ALBアクセスログ有効化',
        description: 'S3バケットを作成してALBアクセスログを保存',
        estimatedEffort: '中'
      });
    }

    // コスト最適化関連の次のアクション
    if (rdsCount > 2) {
      actions.push({
        priority: '低',
        category: 'コスト',
        title: 'Aurora Serverless v2検討',
        description: '利用パターンを分析してServerlessへの移行を検討',
        estimatedEffort: '大'
      });
    }

    return actions;
  }

  /**
   * レビューコメントを生成
   */
  generateReview() {
    const security = this.analyzeSecurity();
    const reliability = this.analyzeReliability();
    const performance = this.analyzePerformance();
    const cost = this.analyzeCost();
    const operability = this.analyzeOperability();
    const nextActions = this.generateNextActions();

    const lines = [
      '## 📊 AI レビュー Summary',
      '',
      `**分析対象**: Terraform Plan JSON`,
      `**変更統計**: 作成 ${this.summary.stats.create}件 / 更新 ${this.summary.stats.update}件 / 削除 ${this.summary.stats.delete}件`,
      '',
      '---',
      '',
      '### 1️⃣ セキュリティ',
      '',
      '**現状**:',
      ...security.current.map(s => `- ${s}`),
      '',
      '**あるべき姿**:',
      ...(security.ideal.length > 0 ? security.ideal.map(s => `- ${s}`) : ['- 現時点で重大な問題なし']),
      '',
      '---',
      '',
      '### 2️⃣ 信頼性',
      '',
      '**現状**:',
      ...reliability.current.map(s => `- ${s}`),
      '',
      '**あるべき姿**:',
      ...(reliability.ideal.length > 0 ? reliability.ideal.map(s => `- ${s}`) : ['- 現時点で重大な問題なし']),
      '',
      '---',
      '',
      '### 3️⃣ パフォーマンス',
      '',
      '**現状**:',
      ...performance.current.map(s => `- ${s}`),
      '',
      '**あるべき姿**:',
      ...performance.ideal.map(s => `- ${s}`),
      '',
      '---',
      '',
      '### 4️⃣ コスト最適化',
      '',
      '**今回の変更によるコスト影響**:',
      ''
    ];

    if (cost.items.length > 0) {
      lines.push('| リソース | アクション | 影響 |');
      lines.push('|---------|-----------|------|');
      cost.items.forEach(item => {
        lines.push(`| ${item.resource} | ${item.action} | ${item.impact} |`);
      });
      lines.push('');
      const sign = cost.totalChange > 0 ? '+' : '';
      lines.push(`**月額コスト変動**: ${sign}$${cost.totalChange}/月`);
    } else {
      lines.push('- コスト影響なし');
    }

    lines.push('');
    lines.push('**コスト削減案**:');
    cost.savings.forEach(saving => {
      lines.push(`- ${saving.name}: **$${saving.amount}/月**`);
    });

    lines.push('');
    lines.push('---');
    lines.push('');
    lines.push('### 5️⃣ 運用性');
    lines.push('');
    lines.push('**現状**:');
    lines.push(...operability.current.map(s => `- ${s}`));
    lines.push('');
    lines.push('**あるべき姿**:');
    lines.push(...operability.ideal.map(s => `- ${s}`));

    lines.push('');
    lines.push('---');
    lines.push('');
    lines.push('## 🎯 次のアクション（小さいPRで完結）');
    lines.push('');

    if (nextActions.length > 0) {
      const highPriority = nextActions.filter(a => a.priority === '高');
      const mediumPriority = nextActions.filter(a => a.priority === '中');
      const lowPriority = nextActions.filter(a => a.priority === '低');

      if (highPriority.length > 0) {
        lines.push('### ❗ 優先度: 高');
        highPriority.forEach((action, i) => {
          lines.push(`${i + 1}. **[${action.category}] ${action.title}**`);
          lines.push(`   - ${action.description}`);
          lines.push(`   - 工数: ${action.estimatedEffort}`);
          lines.push('');
        });
      }

      if (mediumPriority.length > 0) {
        lines.push('### 💡 優先度: 中');
        mediumPriority.forEach((action, i) => {
          lines.push(`${i + 1}. **[${action.category}] ${action.title}**`);
          lines.push(`   - ${action.description}`);
          lines.push(`   - 工数: ${action.estimatedEffort}`);
          lines.push('');
        });
      }

      if (lowPriority.length > 0) {
        lines.push('### 📋 優先度: 低');
        lowPriority.forEach((action, i) => {
          lines.push(`${i + 1}. **[${action.category}] ${action.title}**`);
          lines.push(`   - ${action.description}`);
          lines.push(`   - 工数: ${action.estimatedEffort}`);
          lines.push('');
        });
      }

      lines.push('---');
      lines.push('');
      lines.push('💡 **推奨**: 次のPRでは上記の「小」工数タスクから1つ選んで対応することで、小さいPRでの継続的改善が可能です。');
    } else {
      lines.push('✅ 特に緊急の対応事項はありません');
      lines.push('');
      lines.push('このPRで実装した内容は適切です。次の改善は必要に応じて検討してください。');
    }

    lines.push('');
    lines.push('---');
    lines.push('');
    lines.push('*🤖 AIによる自動分析 - Well-Architected Framework 5つの柱に基づく*');

    return lines.join('\n');
  }
}

const analyzer = new PlanAnalyzer(summary);
const review = analyzer.generateReview();
console.log(review);
