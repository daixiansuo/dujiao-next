# Proposal

## Why

上游商品失效后，列表同步跳过缺失商品、购买校验信任旧缓存，导致用户付款后采购被拒绝。需要以可履约为前提自动停售，并在收款前阻止不可售或无法核验的上游商品。

## What Changes

- 定时库存同步使用完整分页，对缺失商品查询详情，兼容商品详情 HTML 404 自动停售。
- 统一商品及 SKU 停售、库存更新和错误处理；自动停售保留同步映射。
- 正式下单和发起支付共用严格可售核验，预览不请求上游。
- 采购明确拒绝后联动停售，修正库存展示的默认有货降级。
- 保留人工复售，补齐任务摘要、三语错误和关键业务测试。

## Capabilities

### New Capabilities

- `upstream-sale-availability`: 上游商品的同步停售、购买核验及采购拒绝闭环。

### Modified Capabilities

无。

## Impact

涉及 upstream、catalog mapping/product、order、payment、channelapi、procurement、装配和后台文案。复用现有字段、事务、节流及任务，不增加依赖、数据库迁移或新 HTTP 接口。生产部署和历史数据修正不在本次本地实施范围。
