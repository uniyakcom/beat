# 工作流说明

## 文件清单

| 文件 | 作用 |
|---|---|
| `.githooks/pre-commit` | 提交时自动格式化 Go 代码 |
| `.github/release-drafter.yml` | Release Drafter 配置（分类、标签、版本规则、模板） |
| `.github/workflows/release.yml` | push/PR 时自动更新 Release 草稿 |
| `.github/workflows/format.yml` | PR 格式化 + 测试门禁 |

## 初始化

克隆仓库后执行一次：

```bash
git config core.hooksPath .githooks
```

## 日常开发

```bash
git add .
git commit -m "feat: add batch mode"   # pre-commit 自动格式化
git push                                 # 自动更新 Release 草稿
```

## 自动标签

PR 标题会被自动打标签，标签决定版本递增：

| PR 标题关键词 | 自动标签 | 版本变化 |
|---|---|---|
| `feat`, `feature`, `add`, `implement` | `new feature` | minor +1 |
| `fix`, `bug`, `resolve` | `bug` | patch +1 |
| `!:`, `breaking` | `breaking changes` | major +1 |
| `refactor`, `improve`, `update` | `enhancement` | — |
| `opt:`, `perf`, `optimize` | `optimization` | — |
| `doc` | `docs` | — |
| `dep`, `upgrade`, `bump` | `dependencies` | patch +1 |
| `chore`, `misc`, `cleanup`, `ci` | `chores` | — |

## 发布方式

### 1. 发布 Release 草稿（推荐）

每次 push 到 main，Release Drafter 会自动维护一个草稿：

1. 打开 GitHub → **Releases** 页面
2. 查看自动生成的草稿，版本号已根据标签自动计算
3. 可编辑版本号或内容
4. 点击 **Publish release** → 完成！

### 2. 手动指定版本号

编辑草稿时直接修改 Tag 和标题中的版本号。

### 3. 强制版本控制

给 PR 添加 `major:` / `minor:` / `patch:` 标签来覆盖自动推断。

## 格式化

- **本地**：`pre-commit` 钩子在每次 commit 时自动运行 `gofmt`
- **远程**：PR 合入 main 时 `format.yml` 检查格式，未通过则阻止合并

## Release 产物

每次发布自动生成：

- Git Tag（自动创建）
- GitHub Release 页面
- 分类 Changelog（Breaking Changes / Features / Enhancements / Bug Fixes / Documentation / Misc）
- Full Changelog 对比链接
- `go get` 安装命令（仓库地址自动获取）
