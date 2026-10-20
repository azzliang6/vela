# 待启用的 CI

`build.yml` 是新的 MyGo 构建工作流（测试，然后打包 macOS / Windows / Linux）。推送这个 PR 用的令牌没有 GitHub 的 `workflow` 权限，所以没办法在 PR 里直接改 `.github/workflows/`。合并时请用有该权限的账户执行：

```sh
git rm .github/workflows/build-android-apk.yml .github/workflows/build-windows-msi.yml
git mv ci/build.yml .github/workflows/build.yml
```

在这之前，旧的 Android / MSI 工作流仍会引用已删除的 `src-tauri`，因此会失败。
