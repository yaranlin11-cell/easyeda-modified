# 数据手册驱动的自动建库

目标是让用户只提供准确型号或 PDF：Agent 通读手册、提取带页码的证据、生成确定性几何，
再由 CLI 校验并写入 EasyEDA。不要让用户重复选择手册中已经明确的参数；只有封装后缀、
多种 land pattern 或手册缺失等真实歧义才询问用户。

## 工作流

1. 先 `lib search` / `lib by-lcsc` 查现有 Device。型号、封装和引脚定义完全匹配时优先复用，
   不重复建库。
2. 获取原厂 PDF 并通读目录、订购信息、pin table、package drawing、recommended land pattern。
   扫描件先 OCR；表格与尺寸图同时看文字层和页面图。必须锁定完整 MPN 与封装后缀，不能把
   同系列其他 variant 的引脚或尺寸混入。
3. 写一份完整 Device JSON。`evidence` 记录厂家、MPN、封装变体、手册定位信息，以及引脚表、
   外形图、推荐焊盘各自的 1-based PDF 页码。可保存 SHA-256，防止同 URL 文档静默换版。
4. Symbol 使用真实 physical pin number。可按功能分组排布，名称和电气类型来自手册；不因
   多个 GND 同名而合并编号。Footprint 单位为 mil，尺寸换算固定为 `1 mm = 39.37007874 mil`。
5. Land pattern 的依据只能是：`datasheet`（厂家推荐图）、`calculated`（按明确标准/公式计算，
   在 note 写标准、目标密度和关键假设）、`copied-verified`（复制已有封装并在 note 写来源与
   尺寸对账）。器件 body/lead 尺寸不能直接冒充铜焊盘尺寸。
6. 运行 `easyeda lib device validate --spec device.json`。默认要求 symbol pin 与 footprint pad
   编号集合完全相等；机械焊盘等例外必须在 `pinMapping.footprintOnly` / `symbolOnly` 逐项声明。
7. 门禁通过后运行 `easyeda lib device build --spec device.json`。该命令在任何 EasyEDA 写入前
   重跑同一校验，然后创建 Symbol、Footprint、可选 3D Model 和 Device；失败会尝试回滚。
8. 回读 Device 绑定、symbol pins 与 footprint pads，放置一个实例核对 Pin-1/极性/旋转，运行
   原理图检查和 PCB DRC 并保存。`verified:true` 只证明对象成功创建，不证明封装电气/机械正确。

需要保持原始符号几何且避免同名冲突时，可用
`lib symbol copy --uuid <源符号> --source-library <源库> --name <独立名称> --scope personal`。
它调用官方复制接口，回读新符号 UUID、库与名称；复制成功仍不能代替放置后的真实引脚核对。
配合 `lib footprint copy` 将两种资产准备在同一库后，再显式创建 Device，避免跨库绑定时
依赖宿主的同名选择弹窗。

创建 Device 的 `verified:true` 要求设备 UUID、所属库和请求的 symbol/footprint 关联精确一致。
宿主自动复制或复用同名资产导致关联变化时返回 `partial:true, verified:false`，保留已创建 UUID
供核对；不能盲重试，也不能只凭名称放置。旧版自动确认同名库的逻辑已移除。

## 规格契约

从 [library-device-example.json](library-device-example.json) 复制骨架。关键字段：

- `evidence.datasheet.locator` 可为原厂 URL 或本地 PDF 路径；`pinoutPages`、
  `packageDrawingPages` 至少各一页。
- `landPattern.basis=datasheet` 必须给 `pages`；`calculated` / `copied-verified` 必须给 `note`。
- `symbol.geometry` 直接传给 `library.symbol.build`：`outline[]` 至少四个 x/y 点，`pins[]` 的
  `number/name/x/y` 必填。
- `footprint.geometry` 直接传给 `library.footprint.build`：每个 pad 的 `number/layer/x/y/shape`
  必填，`shape` 为官方 pad tuple，类型必须是字符串：`["RECT", 宽, 高, 圆角%]`、
  `["ELLIPSE"|"OVAL"|"NGON", 宽, 高]` 或 `["POLYGON", 多边形源]`；数字类型（如 `[1, w, h]`）
  宿主会在写入中途拒绝，现已在写入前拦截。图形线为 `lines[]`。
- `--library` 用 `lib symbol/footprint create` 回包**顶层**的 `libraryUuid`；工程库资产自身记录里的
  `libraryUuid` 会写成个人库 uuid，照抄它会打不开编辑器（CLI 会拒绝并给出正确值）。
- 封装建在**个人库**（`lib footprint create --scope personal`）：Web 4.1.71 的 API 打不开工程库封装的
  编辑器，build 会被拒绝；符号可建在工程库或个人库。
- pad 可声明 `solderMaskAndPasteMaskExpansion`，仅支持官方 `topSolderMask`、
  `bottomSolderMask`、`topPasteMask`、`bottomPasteMask` 四个有限数值字段，单位 mil、每侧扩展；
  `0` 表示零扩展，省略表示不指定。当前公开 SDK 无法独立回读规则策略，因此拒绝 `null`。
  按焊盘类型及所在面使用支持的字段，
  不把通孔焊盘的钢网字段当作有效开窗。示例：`{"topSolderMask":2,"topPasteMask":0}`。
  普通 `get()` 会把开窗舍入到 0.1 mil；逐 pad 新鲜读取后用公开 `toAsync()` / `reset()`
  恢复精确值并验证请求字段，明确保存后再次验证；字段丢失或 save 未返回布尔 true
  时停止并保留 partial/rollback 证据。`apertureVerifiedPads` 只计明确指定并回读通过的焊盘。
- `library.symbol.build` 与 `library.footprint.build` 只写入刚创建且可证明为空的目标资产。
  Connector 打开编辑器后、创建任何图元前读取目标图元 inventory；发现已有几何或无法完整读取
  inventory 时以 `PRECONDITION_REFUSED` 零写入拒绝。不要对同一 UUID 重放 build；当前不提供
  replace/清空模式，需要重建时删除并重新 create 一个资产，再用新 UUID build。
- 重复 pin/pad number、非有限坐标、非正焊盘尺寸、缺页码证据或未声明的映射差异都会在
  离线阶段拒绝，不打开 EasyEDA。

## 图形与实际接线复核

资产创建和编号校验不等于符号可用。建库后按源规格回读并检查：

- 矩形 outline 显式重复首点闭合；检查引脚线落到边框、名称在框内，不能只核对 rotation 数值。
  `symbol build` 的引脚坐标是外部连接端，rotation 表示离开器件的方向：左侧 180°、
  右侧 0°、顶部 90°、底部 270°。长度向该方向的反方向延伸至边框；
  例如左侧连接端 x=-40、长度 20、rotation=180°，线的另一端应为 x=-20。
  建库回包的数量检查不能证明方向正确，仍需放置后读取引脚并导图核对。
- 引脚线形与电气类型是两个字段。用户要求普通线时用 `shape: "None"`；要求未定义类型时
  用 `pinType: "Undefined"`。不要把此要求推广为所有器件的默认电气类型，也不要因名称含
  CLK/CS 就擅自添加箭头或反相圆圈。低有效语义、1 脚定位标记应分别表达。
- 1 脚定位圆应为独立图形，放在 1 脚一侧；不是把 GND 设置成 `Inverted`。
- NC 不以单个 `noConnected:false` 字段或渲染图推断最终状态。若用户发现实例无法接线，
  先保留用户已完成的 NC 清理，再检查实际实例与接线行为；未验证实例时明确保留验收缺口。
  不把用户确认的 NC 叉号解释成普通连接点，不自动恢复用户删除的标记。
- “装上模块后仍可读”的丝印按模块覆盖范围避让，不只避开焊盘；用实际文字 bbox 检查
  框外位置、行列对齐及文字间距。源规格同步最终文字位置，并说明额外脚本步骤是否被 build 支持。

AS07-M1101D-SMA 现场案例曾出现边框未闭合、引脚朝外、时钟/反相装饰不符用户要求及
NC 误判；最终按用户要求使用普通线与未定义类型。该案例完成库级回读与官方渲染复核，
尚未完成放置实例接线、PCB DRC 或实物装配验证，不能将其视为这些项目已通过。

## 必须暂停的歧义

完整 MPN 对应多个封装、订购后缀无法确定、pin table 与 package 图冲突、推荐焊盘不存在且
计算依据不明确、EP/NC/DNU 的电气处理互相矛盾时，列出候选与证据页再询问用户。图像模糊时
换原厂 PDF 或更高分辨率页面，不凭轮廓猜尺寸。

官方 `eext-ai-library-builder` 的可吸收点是 PDF 转页图、自动定位 pin/package 区域和按封装类型
提取参数；其默认尺寸、一次视觉结果和人工框选都不能作为我们的最终真值。Agent 负责语义
理解，CLI 负责确定性校验和写入。

完整 Device build 的 `properties` 是平铺展示属性（如 `Manufacturer`、`Manufacturer Part`、`Value`），
不是 SDK 的嵌套 property 对象。身份字段由已校验的 `evidence.manufacturer/mpn` 写入；
显式展示身份若与 evidence 冲突则写入前拒绝。失败回包保留 stage、已创建资产 ID 和 dispatchLog；
`rollbackAttempted` 不代表回滚已验证，须核查删除回执及目标存在性。

### 异形焊盘的显式创建路径

`lib footprint build --spec` 的单个 POLYGON pad 可显式设置
`creationMethod: "create-then-modify"`：先创建一个临时矩形，再用官方异步 setter 设置形状，并显式等待 done()
提交完整目标形状。该路径只用于库编辑器，默认仍直接 create，不在失败后自动降级。
转换后及保存后均重新 get 取得形状、编号、位置、层与角度并逐项核对；
不接受临时矩形作为结果，无法证明一致时失败并回滚已知对象。最终仍须重载和原生导出核对。
现场支持状态取实际回包，不由类型声明或单元测试推断。

Web 4.1.78 live probe: the explicit conversion path failed inside official modify
with `Cannot read properties of undefined (reading 'length')`. The temporary
asset was subsequently deleted by exact UUID/name and absence was verified.
POLYGON creation remains unsupported on this host; do not place a real part
based on the offline tests.

For deliberately unbound schematic devices, identity recovery may use the exact
same-export DEVICE origin plus explicit empty Footprint and a known Symbol owner.
Official device.get must confirm the UUID, library, manufacturer/model, supplier
and absence of a footprint. This proves only device identity; missing-footprint
DRC remains visible and PCB transfer is still incomplete. Unknown/missing source
fields, a conflicting footprint, or identity/context drift remain refusals.

异形焊盘提交必须等待完成：Web 4.1.78 的 pad.modify 内部调用 done() 但未等待其 Promise；
改用 toAsync → setState_Pad → await done 的单次显式提交。回读对象必须重新 get，
不能用 reset 当纯读取：该宿主 reset 调用 setState_Hole(null)，可能改变 SMD 的层状态，
并使用不同坐标转换路径。此调用链修复已离线验证；现场多边形提交仍被宿主拒绝，
因此不宣称 POLYGON 建库已支持，不以矩形替代目标铜形。

焊盘参数预检会拒绝边数小于 3 或非整数的 NGON，以及含非有限数、未知指令、空环或混合嵌套结构的 POLYGON。该检查只保证参数结构，不证明轮廓有效或宿主支持创建；现场保存重载核验仍不可省略。


### 尚无封装的器件与异形焊盘转换

符号已连接但封装尚空时，先建立并核验独立的 prepared Device，再用
`sch rebind-footprint --prepared-device-uuid ... --prepared-device-lib ...`。
首次绑定要求原实例来自精确原生 DEVICE 来源，原库明确返回空封装 UUID；
未知、读取失败或半缺失身份不视为空。该路径不修改原库，候选先创建并逐脚验证几何、NC、
型号和关联，再删除原实例；失败恢复原器件、逐脚状态及关联，随后仍须重新读取网络。
原生单单元 token 与库显示名称不同，仅在精确同符号关联或同名单单元映射、唯一单元及完整逐脚几何一致时接受。单元名本身不能替代引脚验证。

宿主拒绝 POLYGON 创建时，可将有来源的参数几何生成独立 KiCad 单板 ZIP，
由真实 KiCad 读取并重新保存，再经 `project import --format kicad` 导入独立验证工程。
自定义 pad 的小锚点必须完全包含在目标铜形内；坐标轴方向需明确转换。
用原生导出与源参数逐顶点、编号、图层及孔属性核对，并保存、重载、再读；
复制所得封装用于主工程后还需核对。导入成功不证明几何正确，铜焊盘验证不覆盖钢网开口。

首次绑定现场发现，宿主在仅恢复身份/属性后可能保留错误的引脚空间索引，出现“线碰脚但网表为空”。
恢复属性必须同时明确原 x/y/rotation/mirror，并在最后一次属性写入后重新检查全部引脚。
即使事务回执成功，仍需逐脚网络检查及保存重载；按原姿态重新应用后网表恢复的结果不能追溯标为此前已通过。
独立命名的符号副本曾解除同符号资产复用时的创建超时；这是可用替代路径，不证明所有超时均由库冲突引起。
