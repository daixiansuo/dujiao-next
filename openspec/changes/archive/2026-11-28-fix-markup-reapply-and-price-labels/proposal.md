# Proposal

## Why

会员价签与实际价格分离，卡片及横表难以辨认。分类加价重算仍固定以 200 条分页读取上游，遇到仅接受 50 条的上游会失败；重算还忽略写入错误并可能回写旧规格状态。

## What Changes

- atelier 卡片及横表将会员、促销等价签统一归入价格区域，保持取价逻辑。
- 公共上游分页复用现有同步分页配置，覆盖分类回填、统计和批量导入。
- 重算仅写价格字段，按商品使用已有事务；写入失败回滚该商品并准确报告失败。
- 保存成功但自动重算失败只显示一次明确提示，补齐三语错误。

## Capabilities

### New Capabilities

无。

### Modified Capabilities

- `connection-pricing`: 分页配置一致、重算写入可靠及错误反馈。
- `storefront-customization`: atelier 价格标签与金额保持在同一区域。

## Impact

涉及 mapping、product SKU 存储、siteconnection 错误映射及前后台展示。不新增数据库字段、依赖或路由；不操作生产，不提交或部署。
