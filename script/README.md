# script —— 一键构建与启动

| 脚本 | 用途 |
| --- | --- |
| `start.sh` / `start.ps1` | 缺 `config.json` 时从示例生成，缺二进制时自动构建，前台启动服务（http://127.0.0.1:8080/web）。 |
| `build.sh` / `build.ps1` | 默认构建 **linux/amd64** 到 `dist/opencode2api`（供 Linux 部署）；`current` / `-Current` 改构建当前平台；`all` / `-All` 交叉编译六个平台并打成 zip 到 `dist/packages/`。`VERSION=v1.0.0` / `-Version` 注入版本号。 |

Go 缓存固定在仓库内 `.local/`（已 gitignore），不依赖系统默认缓存目录。

```bash
script/start.sh                    # 快速启动
script/build.sh                    # 出 linux/amd64 部署包
script/build.sh all                # 出全平台发布包
```

```powershell
.\script\start.ps1
.\script\build.ps1 -All -Version v1.0.0
```
