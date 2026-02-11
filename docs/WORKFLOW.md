# 工作流说明

## 文件清单

| 文件 | 作用 |
|---|---|
| `.githooks/pre-commit` | 提交时自动格式化 Go 代码 |
| `.github/workflows/release.yml` | push 到 main 或打 tag 时自动发布 |
| `.github/workflows/format.yml` | PR 格式化 + 测试门禁 |

## 初始化

克隆仓库后执行一次：

```bash
git config core.hooksPath .githooks
```

## 日常开发

```bash
git add .
git commit -m "feat: add batch mode"   # 自动格式化
git push                                 # 自动发布
```

commit message 遵循 [Conventional Commits](https://www.conventionalcommits.org/)：

| 前缀 | 含义 | 版本变化 |
|---|---|---|
| `fix:` | 修复 | patch +1 |
| `feat:` | 新功能 | minor +1 |
| `feat!:` 或含 `BREAKING CHANGE` | 破坏性变更 | major +1 |
| 其他（`docs:`, `chore:` 等） | 杂项 | patch +1 |

## 发布方式

### 1. 全自动（推荐）

push 到 main，版本号从 commit message 自动推断：

```bash
git push   # → v1.1.0（如果包含 feat: 提交）
```

### 2. 手动指定版本号

```bash
git tag v2.0.0
git push origin v2.0.0
```

### 3. GitHub UI 手动触发

1. 打开 **Actions** → **Release**
2. 点击 **Run workflow**
3. 可选填写版本号，或选择递增类型（patch/minor/major）

### 4. 跳过发布

commit message 中加 `[skip release]`：

```bash
git commit -m "docs: update readme [skip release]"
```

## 格式化

- **本地**：`pre-commit` 钩子在每次 commit 时自动运行 `gofmt`
- **远程**：PR 合入 main 时 `format.yml` 检查格式，未通过则阻止合并

## Release 产物

每次发布自动生成：

- Git Tag
- GitHub Release 页面
- 分类 CHANGELOG（Features / Fixes / Other）
- 安装命令
