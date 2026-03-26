# Template Repository

GitHub Actions CI/CD workflows and project structure template for various projects.

## Purpose

This template repository provides a comprehensive set of CI/CD workflows that can be used across different project types (Terraform, Python, Node.js, Go, etc.). Simply create a new repository from this template and customize the workflows based on your needs.

## How to Use

1. **Create a new repository from this template**
   - Click "Use this template" button on GitHub
   - Or use: `gh repo create <your-repo-name> --template r4sd/template`

2. **Choose workflows you need**
   - Keep only the workflows relevant to your project
   - Delete unused workflow files
   - Customize the remaining workflows

3. **Update configuration**
   - Modify workflow triggers (branches, paths)
   - Update environment variables
   - Configure secrets if needed

## Available Workflows

### Core Workflows

#### `main.yml` - Main CI/CD Pipeline
The orchestrator workflow that runs all checks in parallel and sequence.

**Triggers:**
- Pull requests to `main` or `develop` branches
- Push to `main` branch
- Configurable path filters

**Jobs:**
- Setup environment
- Format check
- Validation
- Linting
- Security scanning
- Plan generation (for infrastructure)
- AI code review
- Summary generation

### Terraform Workflows

#### `_setup.yml` - Environment Setup
- Reads Terraform version from `.terraform-version`
- Sets up environment variables

#### `_terraform-fmt.yml` - Format Check
- Checks Terraform code formatting
- Comments on PR with results
- Fails if formatting issues found

**Usage:** Can be removed for non-Terraform projects

#### `_terraform-validate.yml` - Validation
- Validates Terraform configuration
- Checks syntax and internal consistency

**Usage:** Can be removed for non-Terraform projects

#### `_terraform-plan.yml` - Plan Generation
- Generates Terraform execution plan
- Uses OIDC authentication for AWS
- Uploads plan artifacts
- Comments plan output on PR

**Requirements:**
- AWS OIDC provider configured
- IAM role for GitHub Actions

**Usage:** Can be removed for non-Terraform projects

#### `_tfcmt.yml` - Terraform Plan Comment
- Enhanced plan output using tfcmt
- Better formatted plan comments on PR
- Uses OIDC authentication

**Usage:** Can be removed for non-Terraform projects

### Linting Workflows

#### `_tflint.yml` - TFLint
- Terraform-specific linting
- Checks best practices and potential issues

**Usage:** Can be removed for non-Terraform projects

### Security Scanning Workflows

#### `_security-scan.yml` - Security Scanning
Multiple security scanners run in parallel:

1. **TFSec** - Terraform security scanner
2. **Checkov** - Infrastructure as Code security scanner
3. **Trivy** - Vulnerability scanner for IaC

**SARIF Upload:**
- Results uploaded to GitHub Security tab
- Requires `security-events: write` permission

**Usage:**
- For Terraform projects: Keep all scanners
- For other projects: Consider replacing with language-specific security tools

### Analysis Workflows

#### `_ai-review.yml` - AI Code Review
- Downloads plan artifacts
- Analyzes changes using AI
- Posts detailed review comments on PR

**Requirements:**
- Plan artifacts from `_terraform-plan.yml`

**Usage:**
- For Terraform: Keep as-is
- For other projects: Adapt to review code changes instead of plans

#### `_summary.yml` - Summary Generation
- Aggregates results from all jobs
- Posts summary comment on PR
- Shows pass/fail status for each check

**Usage:** Keep for all projects, customize job names

### Cost Estimation (Optional)

#### `_cost-estimation.yml` - Cost Estimation
- Estimates infrastructure costs using Infracost
- Comments cost breakdown on PR

**Requirements:**
- `INFRACOST_API_KEY` secret

**Usage:** Can be removed if cost estimation not needed

## Workflow Customization Guide

### For Terraform Projects

**Keep:**
- All Terraform workflows (`_terraform-*.yml`, `_tfcmt.yml`)
- `_tflint.yml`
- Security scanning workflows
- `_ai-review.yml`
- `_summary.yml`

**Customize:**
- Update `main.yml` path filters to match your directory structure
- Configure AWS OIDC authentication
- Set Terraform version in `.terraform-version`

### For Python Projects

**Keep:**
- `_setup.yml` (modify to read Python version)
- `_summary.yml`
- Consider keeping `_security-scan.yml` (replace Terraform scanners with Bandit, Safety, etc.)

**Remove:**
- All Terraform-specific workflows

**Add:**
- Python linting (flake8, pylint, black)
- Testing workflows (pytest)
- Type checking (mypy)

### For Node.js Projects

**Keep:**
- `_summary.yml`

**Remove:**
- All Terraform-specific workflows

**Add:**
- ESLint/Prettier workflows
- Testing workflows (Jest, Mocha)
- Build workflows
- Dependency audit

### For Go Projects

**Keep:**
- `_summary.yml`

**Remove:**
- All Terraform-specific workflows

**Add:**
- Go linting (golangci-lint)
- Testing workflows
- Build workflows
- Security scanning (gosec)

## Authentication & Permissions

### OIDC Authentication (for AWS)

Terraform workflows use OIDC for AWS authentication instead of long-lived credentials.

**Required Permissions:**
```yaml
permissions:
  id-token: write  # For OIDC token
  contents: read
  pull-requests: write
```

**Setup:**
See [terraform-common](https://github.com/r4sd/terraform-common) repository for OIDC provider and IAM role configuration.

### GitHub Secrets

Depending on your workflows, you may need:

- `INFRACOST_API_KEY` - For cost estimation (optional)
- Language-specific secrets (API keys, tokens, etc.)

## Directory Structure

```
.
├── .github/
│   ├── workflows/        # GitHub Actions workflows
│   │   ├── main.yml     # Main orchestrator
│   │   ├── _setup.yml
│   │   ├── _terraform-*.yml
│   │   ├── _tflint.yml
│   │   ├── _security-scan.yml
│   │   ├── _ai-review.yml
│   │   └── _summary.yml
│   └── tfcmt.yml        # tfcmt configuration (for Terraform)
└── README.md
```

## Best Practices

1. **Start Minimal**
   - Begin with only essential workflows
   - Add more as needed

2. **Customize Triggers**
   - Adjust `on:` conditions to match your workflow
   - Use path filters to run only when relevant files change

3. **Manage Secrets Securely**
   - Use GitHub Secrets for sensitive data
   - Prefer OIDC over long-lived credentials

4. **Monitor Performance**
   - Review workflow execution times
   - Optimize or parallelize slow jobs

5. **Keep Workflows Updated**
   - Regularly update action versions
   - Monitor for security advisories

## Contributing

If you find issues or have improvements for these workflows, please:
1. Fork this repository
2. Make your changes
3. Submit a pull request

## License

This template is provided as-is for use in your projects.
