# Proposal

## Why

定制分支需要吸收 main 的修复与新版店面，同时保持 IDID 功能。分段加价增加同步和分类定价的歧义，已确认删除。

## What Changes

- 完整整合 main 的最终修复与功能，迁移而非丢弃定制。
- **BREAKING** 删除分段模式和分界价写入，保留百分比及固定金额；旧配置先清点再逐连接决定转换，不自动改写或全量重算。
- classic、vault、atelier 三模板统一覆盖三级分类、卡面、教程、工具与品牌定制，保留各自视觉。
- 保留备份、批量图片、排序、Bot 契约和 GHCR 发布约定。

## Capabilities

### New Capabilities

- `connection-pricing`: 两种加价模式与分类比例、导入同步和旧模式保护。
- `storefront-customization`: 三模板的核心 IDID 定制与官方共享取价流程兼容。

### Modified Capabilities

无已有规范。

## Impact

影响连接、映射、商品、后台和三套店面；Bot 仅在存在契约差异时修改。不提交、推送、部署或写入生产数据。
