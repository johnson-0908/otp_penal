# 发版流程

维护者用。用户只需要 README 里那条 `curl | bash`。

## 一览

```
改代码 → 本地 smoke test → git commit + push
           ↓
     scripts/build.sh  (产出 dist/*.tar.gz + SHA256SUMS)
           ↓
     创建 GitHub Release (Web / gh CLI / curl+PAT 三选一)
           ↓
     上传 4 个文件：2 × tar.gz + SHA256SUMS + install.sh
           ↓
     验证 4 个 URL 200 OK
```

## 版本号约定

语义化版本 `vMAJOR.MINOR.PATCH`：

- `PATCH` (v0.1.0 → v0.1.1)：bug fix、小改动、不破坏兼容
- `MINOR` (v0.1.x → v0.2.0)：新功能、非破坏性变更
- `MAJOR` (v0.x.x → v1.0.0)：破坏性变更（接口改、DB schema 不兼容等）

build.sh 会把版本通过 `-ldflags "-X main.version=$VERSION"` 注入二进制，运行时 `ops-panel version` / `opsctl version` 能看到。

## 1. 代码推送

```bash
cd E:/code/cirico-meeting/ops-panel

# 看看改了啥
git status
git diff

# 后端改动必须先本地过一遍
cd backend && go vet ./... && go build ./... && cd ..

# 前端改动过类型检查
cd frontend && pnpm build && cd ..

# 提交（commit message 风格参考已有历史）
git add <具体文件，不要 git add .>
git commit -m "短标题

若干行正文说明：改了什么 + 为什么。
"

git push
```

**敏感文件别推**。`.gitignore` 已经挡了 `~/.ops-panel/`、`backend/.smoke-data/`、`*.db`、`config.json`、`FIRST_RUN_CREDENTIALS.txt`、`*.pem`、`*.exe`。新增敏感目录记得补 `.gitignore`。

## 2. 构建 release tarball

```bash
cd E:/code/cirico-meeting/ops-panel

# VERSION 必须带 v 前缀，和将来 git tag / release 对齐
VERSION=v0.1.2 bash ./scripts/build.sh
```

产出：

```
dist/
├── ops-panel-v0.1.2-linux-amd64.tar.gz   ≈ 5 MB
├── ops-panel-v0.1.2-linux-arm64.tar.gz   ≈ 4.5 MB
└── SHA256SUMS
```

脚本会自动：

- 前端 `pnpm build`
- 后端 `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION"`（静态二进制，无 cgo 依赖）
- 交叉编译 amd64 + arm64
- 把 `install.sh` / `opsctl` / `ops-panel.service` / 前端静态文件一起打包
- **Windows 上**额外用 Python 重写 tarball，把 `+x` 权限补回二进制（NTFS/MinGW 下 `chmod +x` 对无扩展名 Go 二进制不生效）
- 生成 `SHA256SUMS`

## 3. 创建 GitHub Release（三选一）

### 方案 A：Web UI（最直白，0 配置）

1. 浏览器打开 https://github.com/johnson-0908/otp_penal/releases/new
2. **Tag**: 输 `vX.Y.Z` → 下拉出现 "+ Create new tag: vX.Y.Z on publish"，点它
3. **Target**: `main`
4. **Title**: `vX.Y.Z`
5. **Description**：参考[release notes 模板](#release-notes-模板)
6. **Attach binaries**：拖 4 个文件进去
   - `dist/ops-panel-vX.Y.Z-linux-amd64.tar.gz`
   - `dist/ops-panel-vX.Y.Z-linux-arm64.tar.gz`
   - `dist/SHA256SUMS`
   - `scripts/install.sh` （**每次都要传！** install.sh 可能每版都有改）
7. **不勾 Pre-release**（默认就不勾）
8. 绿色 **Publish release**

**注意**：4 个文件必须等每个都 100% 上传完才能点 Publish，否则 release 会缺文件导致 `curl ... latest/download/` 404。

### 方案 B：`gh` CLI（如果已登录）

```bash
gh auth status  # 确认已登录

cd E:/code/cirico-meeting/ops-panel

gh release create vX.Y.Z \
  dist/ops-panel-vX.Y.Z-linux-amd64.tar.gz \
  dist/ops-panel-vX.Y.Z-linux-arm64.tar.gz \
  dist/SHA256SUMS \
  scripts/install.sh \
  --title "vX.Y.Z" \
  --notes-file docs/release-notes/vX.Y.Z.md
```

**注意**：Windows 上 `gh` 用 schannel 做 TLS，偶尔会 handshake 失败，多试几次即可。

### 方案 C：curl + PAT（最稳，对 SSL 抽风最免疫）

在 https://github.com/settings/personal-access-tokens 新建 fine-grained token：

- Repository access: Only `johnson-0908/otp_penal`
- Permissions → Repository → **Contents: Read and write**
- Expiration: 7 天（发完就撤）

```bash
export GH_TOKEN='github_pat_xxx...'
REPO=johnson-0908/otp_penal
VERSION=vX.Y.Z

# 1. 创建 release，拿到 id
REL_ID=$(curl -sS -X POST \
  -H "Authorization: Bearer $GH_TOKEN" \
  -H "Accept: application/vnd.github+json" \
  -H "Content-Type: application/json" \
  "https://api.github.com/repos/$REPO/releases" \
  -d "{\"tag_name\":\"$VERSION\",\"target_commitish\":\"main\",\"name\":\"$VERSION\",\"prerelease\":false}" \
  | python -c "import json,sys; print(json.load(sys.stdin)['id'])")

echo "release id: $REL_ID"

# 2. 上传 4 个文件
UPLOAD="https://uploads.github.com/repos/$REPO/releases/$REL_ID/assets"
cd E:/code/cirico-meeting/ops-panel

curl -sS -X POST -H "Authorization: Bearer $GH_TOKEN" \
  -H "Content-Type: application/gzip" \
  --data-binary "@dist/ops-panel-$VERSION-linux-amd64.tar.gz" \
  "$UPLOAD?name=ops-panel-$VERSION-linux-amd64.tar.gz"

curl -sS -X POST -H "Authorization: Bearer $GH_TOKEN" \
  -H "Content-Type: application/gzip" \
  --data-binary "@dist/ops-panel-$VERSION-linux-arm64.tar.gz" \
  "$UPLOAD?name=ops-panel-$VERSION-linux-arm64.tar.gz"

curl -sS -X POST -H "Authorization: Bearer $GH_TOKEN" \
  -H "Content-Type: text/plain" \
  --data-binary "@dist/SHA256SUMS" \
  "$UPLOAD?name=SHA256SUMS"

curl -sS -X POST -H "Authorization: Bearer $GH_TOKEN" \
  -H "Content-Type: text/x-shellscript" \
  --data-binary "@scripts/install.sh" \
  "$UPLOAD?name=install.sh"

unset GH_TOKEN  # 别留在环境里
```

如果某次上传 `schannel: failed to receive handshake` 就单独重试那一个文件，别从头开始。

## 4. 验证

```bash
# 4 个 URL 必须全 200
for f in install.sh \
         ops-panel-vX.Y.Z-linux-amd64.tar.gz \
         ops-panel-vX.Y.Z-linux-arm64.tar.gz \
         SHA256SUMS; do
  url="https://github.com/johnson-0908/otp_penal/releases/latest/download/$f"
  code=$(curl -sL -o /dev/null -w "%{http_code}" --max-time 15 "$url")
  echo "$code  $f"
done
```

任何一个不是 200 就回到第 3 步补传（SSL 抽风的情况下单独重试即可）。

## 5. release notes 模板

```markdown
- <改动 1>
- <改动 2>
- Fix: <修了什么 bug>

## Install

\`\`\`bash
curl -fsSL https://github.com/johnson-0908/otp_penal/releases/latest/download/install.sh | sudo bash
\`\`\`

## Upgrade from previous version

\`\`\`bash
opsctl uninstall   # 问到「删数据目录」选 N 以保留现有用户/密码/审计
curl -fsSL https://github.com/johnson-0908/otp_penal/releases/latest/download/install.sh | sudo bash
\`\`\`

（升级流程后面要优化，目标是 `opsctl update` 一条命令就地覆盖，不需要卸载。见 Roadmap。）
```

## 6. 收尾

- **撤销 PAT**（如果用了方案 C）：https://github.com/settings/personal-access-tokens → 找对应 token → **Revoke**
- 本地 `dist/` 可以留着，覆盖式 rebuild 不影响
- 更新 README 如果用户侧有改动

## 排障

| 症状 | 原因 | 解法 |
|---|---|---|
| `curl ... latest/download/install.sh` → 404 | release 里没上传 install.sh，或勾了 pre-release | 编辑 release，补上文件，取消 pre-release |
| `BASH_SOURCE[0]: unbound variable` | 旧版 install.sh，`curl \| bash` 下 BASH_SOURCE 未设 | 升级到 v0.1.1+ |
| `sha256sum: no properly formatted SHA256 checksum lines found` | 旧版 install.sh 的 grep 不认 binary 模式 `*` 分隔符 | 升级到 v0.1.1+ |
| `凭据文件未生成` | systemd unit 没传 `-data-dir`，数据写到了 `/var/lib/ops-panel/.ops-panel/` | 升级到 v0.1.1+ |
| `schannel: failed to receive handshake` | Windows gh/curl 的 TLS 偶发问题 | 直接重试，不是脚本 bug |
| PAT 创建 release 返回 403 | Contents 权限没给 write | 编辑 token → Repository permissions → Contents: Read and write → Update |
| Windows 打出来的 tarball 里二进制没 +x | NTFS/MinGW chmod 对无扩展名文件不生效 | build.sh 里的 Python 后处理已修；install.sh 也防御性 chmod |

## Roadmap

- [ ] `opsctl update` 就地覆盖升级（保留 DB）
- [ ] GitHub Actions 自动构建 + 发版（push tag 就触发）
- [ ] 签名 release（minisign / cosign）
