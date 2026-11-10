# 工程创建、打开、导出与迁移

按任务读取创建与定位、原生导出或外部工程迁移；这些能力分别验证。

## 新建工程与身份确认

从首页调用 MCP `project.create` 时，先通过 `easyeda_health` 选择真实窗口，把新名称放在
`payload.friendlyName`，可用 `payload.open` 请求打开。此动作只创建工程容器，必须提供
`window`，不要传 `project` 或 `doc`；拟建名称不是已有工程，首页标签不是原理图页面。
创建后检查 `created` / `opened` / `partial` 并读回工程身份，再处理文档创建。部分成功时
先用 `easyeda project find --window <id> --name <完整友好名称> --team <teamUuid>`
按友好名称和团队精确查找，不盲目重复创建。它只调用官方项目 UUID 枚举与逐项详情读取；
查找默认等待 90 秒，可显式加 `--timeout 120s`（范围 5 秒到 10 分钟）。超时仍是未知，
不能据此创建同名工程；先保存其他打开文档，用户要求时 typed `web reload` 后再有界重试。
`found` 可用于核对已有工程，`unknown`（例如 UUID 清单为空、详情缺失或枚举报错）不能当作
不存在；只有 `enumeration.complete:true` 且 `presence:"absent"` 才能说明指定团队**根文件夹**内
没有匹配（SDK 不保证递归子文件夹）。本例未传 `folderUuid`，故先查目标团队根文件夹。
结果仍需核对 UUID 和团队，不因同名自动打开或重试创建。无 `--team` 的查找仅
用于发现匹配，不证明全局不存在。返回 `UNKNOWN_ACTION` 时检查连接器是否实现此动作；健康检查的
版本兼容不能证明 handler 存在。其他 MCP 写操作仍要求真实 `project` 和 `doc`；不得推广此例外。

**创建目标要区分个人空间和团队。** 创建个人工程省略 `--team`；`project info` 中可用于
`find --team` 枚举的归属 UUID，不能直接假定也可传给 `create --team`。Web V4.1.60 现场
同名、同参数对照中，显式传个人空间的归属 UUID 立即返回无 UUID；完整回查确认未创建后，
只省略该参数则创建成功，并回查到原归属 UUID。团队工程仍须明确目标团队，失败时不能自动
改为个人工程。遇无 UUID 先完整回查，再根据已确定的目标空间修正参数；不能盲目重试或改归属。

工程级跨项目打开及原生 `.epro2` 导出见 [工程操作](#工程级打开与原生导出)；页面打开不替代工程切换。

## 文档重载与只读就绪核查

`easyeda doc reload <uuid> --project <project> --json` 显式保存、确认关页后只重开一次。
重开使用 30 秒连接器预算（另留响应宽限），成功后在 10 秒内核对活动工程／文档／类型／tab
以及连续稳定的完整组件清单。保存须返回布尔 `saved:true`；关闭、重开或对象读取失败均停止，
不自动重发 open，不用 GUI 刷新兜底。

JSON 回执保留 `saved/reloaded/documentType/activeRestored`，并记录目标、阶段请求 ID、CLI
观察起止时间和 readiness。失败也输出已发生阶段，进程仍非零退出；`saved:true` 而重开失败
是 partial，不等于重载通过。重载非活动文档后，恢复旧活动页必须新读确认；恢复失败单独
报告，已完成的重载事实不丢失。主机观察时间与响应 `createdAt` 分别保留。

超时后可执行纯只读核查：

```bash
easyeda doc ready <expected-document-uuid> --project <project> --timeout 10s --json
```

它不保存、关闭、打开或切页；指定目标必须已活动，省略 UUID 则检查当前页。每次组件读取前后
核对同一上下文，按 `primitiveId` 排序完整组件记录计算指纹；非空清单须连续两次相同，空页
须三次。相同数量而内容不同不算稳定，缺清单、缺身份、重复 ID、上下文漂移或超时均拒绝。
显式 `partial`／`incomplete`、非空 `captureErrors`、不可用或错型的采集状态同样拒绝；
PCB 未请求的焊盘／包络可保留 `not-requested`，该命令不扩大为几何验收。
`--timeout` 为选定窗口后的总读取预算，范围 1 秒至 1 分钟；不会等待队列阻塞自动重试。

`ready:true` 只证明当前组件清单稳定可读，**不证明先前超时 handler 已结束，也不替代 reload
成功、完整网络／导线／几何回读或持久化验收**。保留原失败及保存、关闭、关闭后目标离开活动态
的证据；另作完整新回读并与已保存目标对账。恢复后的新验收周期只执行一次显式 save/reload，
新周期仍失败就停止，不循环重开；不得把原失败改写成成功。

## Allegro 原生 BRD：预览不等于导入

`spec source-probe <file.brd>` 可检测格式标识、SHA256 及内嵌预览分类，不解析完整 PCB。
即使原生 BRD 内有 ZIP/JSON，也不能当作已经转换的工程。当前项目未验证原生 BRD 的
完整导入/回读；官方转换流程见[嘉立创 Allegro 导入说明](https://prodocs.lceda.cn/cn/import-export/import-allegro-orcad/index.html)。
需先通过可用的合法转换工具生成 ebrd 等格式，再单独验证 typed 导入、源/目标数量和几何
映射、保存重开后的完整性；API 类型列出 Allegro 不代表原生二进制已可直接导入。
操作边界与来源追踪见[参考设计](reference-design.md)。

### 不依赖 Allegro 的本地读取路线

KiCad 10 可直接读取 Allegro 二进制 BRD。先查实际版本和 `kicad-cli pcb import --help`，
使用 `--format auto`（10.0.7 的显式格式列表不含 allegro，但自动探测可读取）。
仓库 `scripts/convert-allegro-kicad.py` 封装本地转换、源文件哈希、导入报告与单板 ZIP；
输出目录必须不存在，状态为 converted-review-required，不代表完整性或可制造性。

特别限制：KiCad 10.0.7 的 Allegro `buildVia` 将过孔起止层设为外层，盲埋孔层对不能据此验收；
板厚也可能保留默认值。不能仅靠导入报告 warnings=[] 判断无损，必须独立核对层对、厚度、
材料、焊盘、铺铜与网络。未知层对禁止依据孔径、坐标重叠或层名猜测修复。

已加入 `easyeda project import --format kicad --file reference-kicad.zip --name <独立参考工程> --window <id>`。
ZIP 只允许一个根目录 .kicad_pcb，传输最大 16 MiB，展开最大 128 MiB；Go/连接器均检查格式，
仅创建新工程，contentVerified=false。文件版本兼容性、导入保真度与现场保存重载另行验证。
复杂板可导致宿主页面崩溃；发生超时或断连后查找目标工程，不重复提交。

#### 转换后的逐项验收与已验证的兼容边界

读取器与宿主导入器是两个独立边界。较新的 KiCad Allegro 读取器可从原始焊盘栈的
`m_StartLayer` 和层数恢复盲埋孔，但版本号本身不证明该能力；固定源码提交、可执行文件
哈希及输入哈希，记录每组实际层对，并用重新读取后的钻孔报告对账。更新读取器也可能
带来新的文件语法，不可直接推断嘉立创支持。

`scripts/convert-allegro-kicad.py` 记录转换器哈希和输出对象清单。离线参考可用
`scripts/prepare-kicad-reference.py --board <pcb> --stackup <recipe.json> --output-dir <new-dir>`
应用有来源哈希及页码的制造要求。recipe 必须明确 `provenanceKind: requirement`、
`sourcePath/sourceSha256/locator`、`copperUm/dielectricUm/maskUm/finishedThicknessUm`、
`dielectricTypes/dielectricMaterials/maskMaterial`；厚度必须为正且逐层合计相符。
已有叠层拒绝覆盖。程序检查除叠层和板厚外的全部语法树保持原样，再用真实 KiCad
重新读取、导出钻孔及 IPC-2581 验证；不能把按要求设置的厚度称作从 BRD 恢复的观测。
未明确的介电常数、损耗角和制造公差不得采用导入器默认值作为已满足要求的证据。

`scripts/prepare-kicad-import.py` 是**实验性兼容候选生成器**，不是完整格式降级器：
- 新版字符串网络引用转换为先于对象声明的编号网络表；否则现场可出现网络全部为空。
- 单位缩放的器件 `transform` 转为 `at`；其他缩放、镜像和未知变换拒绝处理。
- 仅严格识别的四圆角矩形自定义焊盘可转换为 roundrect，识别容差 2 nm；任意自定义形状不猜测。
- 埋孔使用兼容的 blind 标记并保留明确层对；必须回读实际盲埋孔规则的起止层 ID。
- 保留所有区域与已填充铜；不删除铺铜缓存来制造导入成功。大体量区域导入仍有宿主崩溃记录，
  **完整板导入未通过**，不得自动重放同一失败文件或把候选标为 live-verified。

诊断时可在隔离副本中减少对象定位失败，名称与清单必须注明不完整；该工程不能作为成品。
只按对象数量验收仍不够：`scripts/check-kicad-live-reference.py` 对比转换参考 inventory 与
`pcb dump --include-copper` 的逐脚网络、位置和对象数量，检查缺测，不把未知读成零。
其通过也只覆盖所声明的部分；轮廓、区域铜、规则、叠层、DRC 和制造仍需分别核实。
保存重载后的原生导出中，应核对 `LAYER_PHYS` 逐层厚度/材料、盲埋孔规则的起止层 ID，
以及每个 VIA 引用的规则。未经证明的铜完整性禁止进入整板布线或制造交付。



## Altium Designer 工程：当前 `unsupported`

当前没有经过验证的 typed action 或 CLI 能把 `.SchDoc` / `.PcbDoc` 程序化导入
EasyEDA Pro。Agent 不得用交互界面、CUA、`debug.exec_js`、分块 base64 上传或私有消息总线
兜底。需要导入时停止现场写入，将能力标为 `unsupported`，先实现并验证 typed import。

如果目标工程已经由用户独立提供，easyeda-agent 只从现有工程开始读取与核验：

1. `easyeda health --project <project>` 与 `easyeda doc ls --project <project>`：确认连接器、
   目标工程，以及导入所得的原理图/PCB 文档。
2. 对每张原理图读取 `easyeda sch connectivity --page <name-or-uuid> --project <project>`，
   核对器件身份、全部物理引脚、网络、NC、分层页面与位号；再逐页运行适用的
   `layout-lint`、`sch check`、`bridge-check` 和 SDK DRC，分别保存结果。
3. 对 PCB 核对 Board 绑定、器件与焊盘网络、板框、层叠、机械层/禁布区、关键网和
   丝印；运行 `easyeda pcb layout-lint`、`easyeda pcb check --strict` 与
   `easyeda pcb drc --strict`。AD 机械层到 EasyEDA
   层的映射必须在原生画布和数据回读中确认，不能只看工程已出现在目录里。
4. 记录导入前后的数量与关键不变量。缺页、空读、板框/机械层变化、网络不一致或检查
   未运行时，均不能报告迁移成功；修复完成后显式保存。

## 为什么不能调用名义上的导入 API

官方 beta 方法 `eda.sys_FileManager.importProjectByProjectFile` 的签名列出了
`'Altium Designer'` / `'Protel'` 类型，但在 issue #203 的 EasyEDA Pro 桌面端
3.2.149（Windows x64，本地工作区）实测中，对 `.epro2`、`.eprj2` 和真实 `.SchDoc`
都会快速 resolve 为 `undefined`，不抛错，也没有创建文档或改变工程。

因此：

- 方法存在、Promise resolve 或返回 `undefined` 均不算成功；不得继续后续自动化。
- `getAllProjectsUuid()` 在本地工作区可能返回 `[]`，不能单独作为导入副作用判据。
- `eda.sys_FormatConversion.convertAltiumDesignerLibrariesToEasyEDA*` 只转换
  `.SchLib` / `.PcbLib` 库文件，不转换 `.SchDoc` / `.PcbDoc` 工程文档。
- 当前不封装 `import.ad` typed action；原生归档导入见下文 `project import`，状态 `planned`。将来只有在受支持宿主上证明真实副作用，并能在
  返回值缺失、超时和部分导入时明确失败，才可进入 CLI/action 实现。

未来接口封装至少要在写入前记录目标工程指纹，并在返回后核对工程/文档身份、文档清单、
原理图连接数据及 PCB 板框/机械层；不得把 `undefined`、无变化或单一 UUID 清单当成功。

来源与完整探测矩阵见 [GitHub issue #203](https://github.com/zhoushoujianwork/easyeda-agent/issues/203)。


## 工程级打开与原生导出

`project open --uuid` 是旧的文档打开别名，只能用于当前工程内页面，不是跨工程打开。
跨工程时先保存所有未保存文档，再显式调用：

```bash
easyeda project open --window <window-id> --project-uuid <project-uuid> --page-uuid <page-uuid> --allow-discard-unsaved
easyeda project export --window <window-id> --project-uuid <project-uuid> --out ./deliverable.epro2
```

打开命令使用官方 `dmt_Project.openProject`；该 API 可能丢弃未保存数据，因此标志是明确确认，
不是自动保存。命令核对打开后的真实工程身份；超时或失败先读回，不盲目重复。
导出使用官方 `sys_FileManager.getProjectFile`，要求目标工程已激活，前后检查 UUID，
拒绝覆盖文件，校验原生 ZIP 完整性并输出字节数/SHA-256。输出 `restoreVerified=false`：
ZIP 校验和导出成功不代表重新导入验证通过。请在导出前显式保存所有文档。
底层调用正式 typed action `project.open` / `project.export`，需要注册这两个 action 的新版 daemon 及包含 handler 的新版连接器。旧 daemon 或连接器拒绝 unknown action / UNKNOWN_ACTION 时停止并升级；禁止回退到 debug.exec_js。
直接调用 `project.open` 同样必须传 `allowDiscardUnsaved:true`；`project.export` 不接受页面路由。
限制：归档最大 16 MiB，解压验证上限 128 MiB，超限明确失败。

MCP 使用 `easyeda_project_transfer`，`operation` 为 `open` 或 `export`，均需 `window`、
`projectUuid`；打开另需 `allowDiscardUnsaved=true`，导出另需新 `out` 路径。不传文档路由。
此工具需要含上述 CLI 命令的匹配构建；不要仅替换 MCP 而仍使用旧 CLI。

工程身份可能先于文档树就绪。需直接进入原理图时，打开命令同时传 `--page-uuid`（MCP `pageUuid`），等待目标页面出现在树中后只打开一次，并核对工程和页面身份。省略此参数只保证工程身份，不保证页面已加载。

## OrCAD EDIF 原理图导入

`easyeda project import --format orcad-edif --window <id> --file design.edf --name <新工程名>`
使用同一个 typed `project.import`，显式传 `fileType: "OrCAD"`，调用官方导入器。
仅接收 OrCAD 导出的 `.edf` / `.edif` 文本；不能传二进制 `.DSN`、Library XML 或只改后缀。
CLI 和连接器检查 EDIF 外层结构；这不证明语义、图形或连接完整。
默认原生导入逻辑不变。仅创建新工程，导入前保存打开文档；失败、空返回、断线或超时先按名称回查，禁止重复导入。
导入后逐页对照源 EDIF 的器件、全部引脚及跨页网络，并执行保存、重载、新回读；完成前保持 `contentVerified:false`。
PDF 中嵌入图片、OrCAD 私有属性及 PCB 封装几何不保证包含在 EDIF 中，需另行核对。封装名称不等于焊盘几何。

Web 4.1.78 / connector 1.14.0 本地开发构建（2026-10-28）定向验证：官方 OrCAD
EDIF 导入创建四页工程；源文件与重载后官方逐脚回读的 44 个物理位号、513 个引脚一致，
501 个连接脚和 12 个显式 NC 保留。该结果仅覆盖原理图连接迁移；未绑定封装、未完成 PCB/DRC。
EDIF 丢失的嵌入图需从原 PDF 提取后通过 `sch image create` 补回，并单独保存重载回读。
多单元器件共享位号会触发现有 `sch connectivity` IR 的 duplicate component 拒绝；
不要修改真实位号来绕过。可逐页 `sch list --include-pins --include-wires`，保留各单元真实
身份并按物理位号/引脚与源 EDIF 对账。未知、歧义与缺脚均为失败。
官方整工程 PDF 导出在该宿主返回 ZIP，且发现 PDF 文字渲染缺失；检查真实文件类型和
逐页图面，不凭扩展名交付。原生工程与已核验 PNG 保留，必要时另制明确标注为图像版的 PDF。

## 原生归档导入

```bash
easyeda project import --window <window-id> --file ./design.epro2 --name <新工程友好名>
```

调用 `project.import`（官方 @beta `importProjectByProjectFile`，类型 `EasyEDA Pro`），只创建
**新工程**，不写入已有工程。CLI 先校验 ZIP/CRC（`.epro2` 须含 `.epru`），上限同导出；
团队默认取当前工程的团队，可用 `--team-uuid` 指定；`--extract-libraries` 才把内嵌库提取到团队库。

Web 4.1.71 现场结果（2026-10-13，连接器 1.10.5）：

| 输入 | 结果 |
|---|---|
| 编辑器导出的 `.epro2`（118 个有效文档） | 9 秒建成新工程；打开后再导出，逐文档记录（忽略 ticket）118/118 相同，`live-verified` |
| eprj3 文件夹打成 `.zip`（含或不含顶层目录） | 立即返回空，完整枚举确认未建工程：`unsupported` |
| eprj3 文档重新封装为 `project2.json` + 单个 `.epru` | 建成新工程，但宿主随即切到新工程并重载页面，调用以“连接器断开”结束 |

因此导入可能**切走当前窗口的活动工程**：先保存所有文档，且不要在别的会话正在使用的窗口里导入。
断开、空返回或超时都是**结果未知**（参考 [#203](https://github.com/zhoushoujianwork/easyeda-agent/issues/203)
桌面端空返回）：先 `project find --name <名称> --team <teamUuid>`，不得盲目重导入；结束后用
`project open` 切回原工程并核对。回执 `contentVerified:false`：新工程必须打开并回读原理图、PCB 与 DRC。


#### 大型填充铜的实验性分块

`scripts/partition-kicad-copper.py --source <compatibility.kicad_pcb> --out <new-dir>`
使用 Shapely 2.x 分解带零面积桥接线的填充轮廓，沿孔洞内部切分为不带孔洞的小多边形。
保持区域定义、网络、层及其他对象；不重新铺铜，不做轮廓平滑。
先比较归一化面积，再比较分块前后铜集合的对称差，序列化后再比一次。
每个大轮廓的浮点面积误差限为 1e-7 mm²；它是数值校验限，不是制造公差。
遇弧线缓存、不明确的交叉或保留孔洞则拒绝。依赖只安装在隔离目录，不修改用户全局 Python。
分块几何验证通过仅代表离线铜集合保持，宿主导入及全部区域、填充铜、禁布区必须另行回读。
不要将去除缓存的诊断工程当作已有完整铺铜。


## 精密尺寸和整圆禁布区的导入核验

过孔 SDK getter 可能取整。先用 `pcb via-list --native` 读取与当前 PCB 身份绑定的原生 VIA，
再用源码仓库的 `scripts/check-native-vias.py` 对比参考文件的逐孔网络、坐标、孔径、外径、实际层对。
匹配容差显式记录（默认 3 nm），不是制造允许公差；不得用 SDK 读数直接生成尺寸修复计划。

KiCad 单段 `start=end` 整圆 keepout 可能被导入器忽略。`prepare-kicad-import.py` 把严格识别的
整圆改写为四段同圆四分之一圆弧，不作折线逼近；规则和图层不变。保存重载后用
`scripts/check-circle-keepouts.py` 检查每个图层实例的圆心、半径及禁止规则。
该检查不覆盖非圆形区域，也不证明实际铺铜避让成立。

## 原生缓存铜转换实验

KiCad 完整铺铜缓存导入失败时，可在离线副本中测试原生工程转换。源码仓库的
`scripts/compose-native-reference-copper.py` 必须以同一参考板已核验的原生导出为基线，
结合参考 KiCad 的真实 `filled_polygon` 生成候选 `.epro2`；只能经 `project import` 导入独立新工程。
保留输入哈希、被替换的区域铜数量和其他记录逐字相同的检查，不能使用在线源码 setter 改板。

此路径将缓存铜表示为静态填充，**不保留原铺铜区的自动重算语义**。零线宽、复杂孔洞、
网络及层属性都必须经宿主导入、保存、重载、新导出验证；未通过不能称迁移成功。
`--pilot-caches` 只用于小范围能力试验，输出明确为缺铜的诊断工程。完整缓存的面积等价、
连接性、DRC 和制造要求仍须独立检查，不能用文件生成成功代替现场结果。

设计页关闭后允许当前页变成身份一致的符号/封装库编辑器，此事实仅证明原目标已不再活动，
不把库页当作设计数据已就绪。随后重开仍须核对原工程、原目标页和稳定完整器件库存。
