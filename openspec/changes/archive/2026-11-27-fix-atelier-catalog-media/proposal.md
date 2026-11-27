# Proposal

## Why

atelier 的分类树在首页纵向撑开，商品图仍按小图标展示，无法承载 IDID 的横向卡面。需要让新模板完整兼容既有分类与商品展示风格。

## What Changes

- 多级分类改为逐级横向筛选，支持父类全部商品、三级链接恢复和手机换行。
- 精密卡片显示完整横向封面；规格横表使用纯文字。
- 商品详情扩大卡面展台，复用旋转、暂停、多图切换和坏图降级。
- 购物车与结算使用完整横向缩略图，保留现有取图和交易逻辑。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `storefront-customization`：明确 atelier 分类筛选与横向卡面的展示要求。

## Impact

仅影响前台 atelier 的分类、商品列表、详情、购物车与结算展示；沿用现有 Vue 组件、分类工具和 composable，无接口、价格规则、数据库及依赖变更。旧测试库缺失的上传文件单独说明，以可用图片及坏图两组数据验收。
