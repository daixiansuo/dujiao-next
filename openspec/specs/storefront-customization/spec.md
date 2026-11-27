# storefront-customization Specification

## Purpose

规定 IDID 店面在 classic、vault 和 atelier 三种视觉模板中的核心用户行为，保证接入上游新布局后分类导航、商品展示、内容入口和交易价格的连续性。

## Requirements

### Requirement: 三模板核心定制一致
三模板 SHALL 支持三级分类、父级后代筛选和深层链接恢复；商品主卡面完整展示并保留旋转能力；教程支持分类筛选；工具提供本地 TOTP 和文本处理；页脚保留 IDID 品牌、Email/X 并去推广。

#### Scenario: 切换模板
- **WHEN** 用户分别使用 classic、vault 和 atelier 浏览三级分类商品并进入工具和教程
- **THEN** 核心功能均可用，各模板保留独立视觉，不修改管理员当前模板设置

### Requirement: 官方交易修复兼容
系统 SHALL 采用官方最终共享取价逻辑，保留专属价、活动与会员折扣、批发及分销隔离规则，以及移动端购买栏和购物车修复。自定义排序保持现有基础展示价口径。

#### Scenario: 查看并购买商品
- **WHEN** 用户从卡片进入详情、预览和结算
- **THEN** 同条件下价格一致且移动端可操作购买，移除 SKU 后可再次加入
