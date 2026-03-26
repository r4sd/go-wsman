#!/usr/bin/env node

/**
 * Terraform Plan JSON から必要な情報のみを抽出してトークン消費を削減
 *
 * Usage: node extract-plan-summary.js plan.json
 */

const fs = require('fs');

if (process.argv.length < 3) {
  console.error('Usage: node extract-plan-summary.js <plan.json>');
  process.exit(1);
}

const planPath = process.argv[2];
const plan = JSON.parse(fs.readFileSync(planPath, 'utf8'));

/**
 * リソース変更から重要な情報のみを抽出
 */
function extractKeyChanges(resourceChange) {
  const { type, name, change } = resourceChange;
  const action = change.actions[0]; // create, update, delete, replace

  const keyChanges = {};

  // RDS関連
  if (type.includes('rds')) {
    if (change.after) {
      keyChanges.instance_class = change.after.instance_class;
      keyChanges.multi_az = change.after.multi_az;
      keyChanges.storage_encrypted = change.after.storage_encrypted;
      keyChanges.backup_retention_period = change.after.backup_retention_period;
    }
    if (change.before && change.after) {
      // 変更検出
      if (change.before.instance_class !== change.after.instance_class) {
        keyChanges.instance_class_change = {
          from: change.before.instance_class,
          to: change.after.instance_class
        };
      }
    }
  }

  // ECS関連
  if (type.includes('ecs_service')) {
    if (change.after) {
      keyChanges.desired_count = change.after.desired_count;
      keyChanges.enable_execute_command = change.after.enable_execute_command;
    }
    if (change.before && change.after) {
      if (change.before.desired_count !== change.after.desired_count) {
        keyChanges.desired_count_change = {
          from: change.before.desired_count,
          to: change.after.desired_count
        };
      }
    }
  }

  // Auto Scaling関連
  if (type.includes('autoscaling_group')) {
    if (change.after) {
      keyChanges.min_size = change.after.min_size;
      keyChanges.max_size = change.after.max_size;
      keyChanges.desired_capacity = change.after.desired_capacity;
    }
    if (change.before && change.after) {
      ['min_size', 'max_size', 'desired_capacity'].forEach(key => {
        if (change.before[key] !== change.after[key]) {
          keyChanges[`${key}_change`] = {
            from: change.before[key],
            to: change.after[key]
          };
        }
      });
    }
  }

  // Security Group関連
  if (type.includes('security_group')) {
    if (change.after) {
      keyChanges.ingress = change.after.ingress;
      keyChanges.egress = change.after.egress;
    }
  }

  // IAM関連
  if (type.includes('iam_')) {
    if (change.after && change.after.policy) {
      keyChanges.has_policy = true;
    }
  }

  // CloudWatch関連
  if (type.includes('cloudwatch')) {
    if (change.after) {
      keyChanges.log_retention = change.after.retention_in_days;
    }
  }

  return keyChanges;
}

/**
 * コスト影響を推定
 */
function extractCostImpact(plan) {
  const costs = [];

  plan.resource_changes.forEach(rc => {
    const action = rc.change.actions[0];

    // RDS
    if (rc.type === 'aws_rds_cluster_instance') {
      const instanceClass = rc.change.after?.instance_class || 'unknown';
      const baseCost = instanceClass.includes('t3.medium') ? 120 : 200;

      if (action === 'create') {
        costs.push({
          resource: `RDS ${rc.name}`,
          type: 'rds',
          action: 'create',
          monthly_cost: baseCost
        });
      } else if (action === 'delete') {
        costs.push({
          resource: `RDS ${rc.name}`,
          type: 'rds',
          action: 'delete',
          monthly_cost: -baseCost
        });
      }
    }

    // ECS Fargate
    if (rc.type === 'aws_ecs_service') {
      const desiredCount = rc.change.after?.desired_count || 1;
      const taskCost = 30; // 1タスクあたり概算

      if (action === 'create') {
        costs.push({
          resource: `ECS ${rc.name}`,
          type: 'ecs',
          action: 'create',
          monthly_cost: taskCost * desiredCount
        });
      } else if (rc.change.before && rc.change.after) {
        const oldCount = rc.change.before.desired_count || 0;
        const newCount = rc.change.after.desired_count || 0;
        if (oldCount !== newCount) {
          costs.push({
            resource: `ECS ${rc.name}`,
            type: 'ecs',
            action: 'update',
            monthly_cost: taskCost * (newCount - oldCount)
          });
        }
      }
    }
  });

  return costs;
}

/**
 * Well-Architected 分析用のデータを抽出
 */
function extractWellArchitectedData(plan) {
  const data = {
    hasEncryption: false,
    hasMultiAz: false,
    hasBackup: false,
    hasMonitoring: false,
    hasLogging: false,
    hasAutoScaling: false,
    securityGroups: [],
    iamPolicies: [],
    rdsInstances: [],
    ecsServices: []
  };

  plan.resource_changes.forEach(rc => {
    const { type, change } = rc;

    // 暗号化
    if (change.after?.storage_encrypted || change.after?.encrypted) {
      data.hasEncryption = true;
    }

    // マルチAZ
    if (change.after?.multi_az) {
      data.hasMultiAz = true;
    }

    // バックアップ
    if (change.after?.backup_retention_period && change.after.backup_retention_period > 0) {
      data.hasBackup = true;
    }

    // モニタリング
    if (type.includes('cloudwatch') || change.after?.enable_container_insights) {
      data.hasMonitoring = true;
    }

    // ロギング
    if (type.includes('log_group') || change.after?.access_logs) {
      data.hasLogging = true;
    }

    // Auto Scaling
    if (type.includes('autoscaling')) {
      data.hasAutoScaling = true;
    }

    // RDS
    if (type.includes('rds')) {
      data.rdsInstances.push({
        name: rc.name,
        action: change.actions[0],
        instanceClass: change.after?.instance_class,
        multiAz: change.after?.multi_az,
        encrypted: change.after?.storage_encrypted
      });
    }

    // ECS
    if (type === 'aws_ecs_service') {
      data.ecsServices.push({
        name: rc.name,
        action: change.actions[0],
        desiredCount: change.after?.desired_count
      });
    }

    // Security Groups
    if (type.includes('security_group') && !type.includes('_rule')) {
      data.securityGroups.push({
        name: rc.name,
        action: change.actions[0]
      });
    }

    // IAM
    if (type.includes('iam_')) {
      data.iamPolicies.push({
        name: rc.name,
        action: change.actions[0]
      });
    }
  });

  return data;
}

// サマリーを生成
const summary = {
  resourceChanges: plan.resource_changes.map(rc => ({
    address: rc.address,
    type: rc.type,
    name: rc.name,
    action: rc.change.actions[0],
    keyChanges: extractKeyChanges(rc)
  })),
  costs: extractCostImpact(plan),
  wellArchitected: extractWellArchitectedData(plan),
  stats: {
    create: plan.resource_changes.filter(c => c.change.actions.includes('create')).length,
    update: plan.resource_changes.filter(c => c.change.actions.includes('update')).length,
    delete: plan.resource_changes.filter(c => c.change.actions.includes('delete')).length,
    replace: plan.resource_changes.filter(c => c.change.actions.includes('delete') && c.change.actions.includes('create')).length
  }
};

console.log(JSON.stringify(summary, null, 2));
