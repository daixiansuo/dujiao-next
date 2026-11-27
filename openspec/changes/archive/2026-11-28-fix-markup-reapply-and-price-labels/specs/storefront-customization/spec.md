# Spec Delta

## ADDED Requirements

### Requirement: atelier 价签归属价格区域
atelier 卡片和规格横表 SHALL 将会员、促销及批发等价签与实际价格放在同一区域，标题和分类不展示优惠价签；手机端价签保持可见，长标题和价格不得撑宽页面。取价逻辑及售罄状态表达 SHALL 保持现有行为。

#### Scenario: 浏览会员优惠商品
- **WHEN** 用户在桌面或手机切换精密卡片与规格横表
- **THEN** 优惠价签随价格对齐，标题及分类不夹带优惠价签，选购入口正常。
