# 离线实验记录与失败诊断

适用于比较工作流、保存修复尝试和回归既有检查器。`experiment` 不操作 EDA；
它不替代 `apply`、保存重载或最终设计验收。需要 Python 3.9+，几何检查沿用原检查器依赖。

## 冻结输入与记录尝试

`easyeda experiment init --from baseline.json --out new-run`

```json
{"mode":"reference-migration","requirements":["requirements.json"],"inputs":["reference.brd"],"requiredChecks":["native-vias","copper-persistence"]}
```

任务模式另支持 `independent-design`。路径相对于配置文件。新目录不可覆盖。
代码基线应另保存归档与哈希，并作为输入登记；版本字符串是调用者声明，不代表已核验源代码。

`easyeda experiment record --run new-run --from trial.json`

```json
{"id":"x1-y1","workflowVersion":"x1","verifierVersion":"v1","hypothesis":"核对导入后的过孔层对","candidate":"candidate.epro2","evidence":["via-readback.json","rules.json","layers.json"],"evidenceKind":"historical","projectUuid":"project-id","documentUuid":"pcb-id"}
```

`parent` 可引用已存在尝试。证据类型为 `historical`、`fresh-offline` 或 `live-capture`；
它是显式来源声明，工具不会仅凭标签认定现场验收。提供工程身份时必须同时提供文档身份。
工具复制文件、记录 SHA256，元数据另带哈希。哈希用于完整性检测，不是防恶意重写的数字签名。

## 运行已有检查器

`easyeda experiment check --run new-run --trial x1-y1 --checker native-vias --inputs inputs.json --repo /path/to/source-checkout`

```json
{"board":"inputs0.kicad_pcb","native":"trials/x1-y1/evidence0.json","rules":"trials/x1-y1/evidence1.json","layers":"trials/x1-y1/evidence2.json"}
```

输入映射路径相对于实验根目录，只允许当前尝试和冻结基线中的文件。示例文件名以实际清单为准。

| checker | 输入键 | 范围 |
|---|---|---|
| reference | from | 参考资料准备度 |
| chains | from, observed | 已提供物理连接图；不自动提取铜图 |
| constraints | from, observed | 有来源的数值约束 |
| native-vias | board, native, rules, layers | 逐孔位置、尺寸、网络和层对 |
| circle-keepouts | board, regions, layers | 圆形禁布区图层实例 |
| copper-persistence | candidate, manifest, reloaded | 原生铜区保存一致性 |
| copper-gerber | board, export | 诊断 Gerber 铜区几何，受支持语法限制 |
| live-reference | source-inventory, live-dump | 现有参考对象对账范围 |

前三项调用当前 CLI 内置 spec 检查器，后五项运行源码仓库固定脚本；不允许配置任意 shell。
记录检查器与 Python 版本、输入哈希、stdout/stderr 和原始 JSON。超时 120 秒后保留 incomplete。
输入文件中的 sources 和 Gerber artifactPath 依赖必须一并登记。原文件仍存在且哈希匹配时使用
原路径执行，以保留相对引用；原路径失效且引用无法解析时拒绝，不静默改写原始回执。
`PYTHONPATH` 可指定隔离几何依赖；依赖环境未完全锁定，不宣称跨机器逐字节复现。

## 核验、诊断与恢复

- `easyeda experiment verify --run new-run`：校验已登记文件、检查报告和必需检查覆盖率。
- `easyeda experiment diagnose --run new-run --trial x1-y1`：只读输出连接/工具/保存/验证/缺测观测及下一步调查。

失败或缺测非零退出。`pass` 仅代表已声明离线检查通过；始终 `completeAcceptance:false`。
日志截断、路径逃逸、错误工程身份、报告改动不能升级为通过。历史检查保持历史标签。
重复错误只建议复查策略，不证明根因；本阶段不自动修改规则、修板或生成工作流代码。

每个尝试与检查是独立不可覆盖目录。失败检查如需重跑，创建新尝试并设置 parent；
已经完成的尝试可以继续补未运行的检查。中途断电遗留 `.lock`/`.pending-*` 时会拒绝完整验收，
先确认无活动进程并人工核对遗留记录；目前不提供自动清锁或自动恢复半次检查。

## 部分连接图提取

`easyeda experiment extract --from reference.kicad_pcb --out new-graph.json --repo /path/to/source-checkout`

从板文件直线及圆弧提取同层、完全相同坐标的端点连接，过孔只连接已声明层段；不按网名合并。
输出兼容 chains 输入，但固定 complete:false、coverage:[]，退出码 1 表示未完成。
交叉与线宽接触、焊盘及过孔环接触、铜区、模块内部及焊点尚未覆盖；不能据此判开路或整板通过。
独立链路顺序必须来自需求证据，不允许从待测图反推一份必然通过的期望。

live-reference 的 partial-check-passed 只在该适配器映射为离线范围内 pass；
原始报告及其未验证范围仍保留，其他检查器不能使用此状态获取通过。
搬迁后的资料可生成显式标注的路径重定位副本：核对原源文件 SHA256，记录原/新路径和副本哈希；
不得更改冻结原件或把路径修复标为新现场采集。

大板 DRC 报告可能超过 32 MiB；WebSocket 与 CLI 响应读取均采用有限 128 MiB 上限，超过上限仍拒绝，不能返回部分结果作为完整报告。诊断 Gerber 解析支持圆形光圈直线笔画，但 fills-only 导出发现区域外笔画仍判失败；其缓冲几何仅用于定位，不作为制造精度验收。

对象对账检查器支持显式 `--native current.epro2 --document pcb-uuid`：仅当实例位置/角度一致、唯一 Footprint 引用指向非空原生 FOOTPRINT 文档、且库内无 PAD/嵌套 COMPONENT 时，补充零焊盘证据。保留 SDK 原回执；此路径只证明该原生快照的空焊盘清单，不证明图形层映射或制造验收。当前需直接运行脚本，experiment 适配器尚未接入此可选证据。

## 原生铜区的精确诊断导出

`easyeda experiment export-copper --from config.json --out new.zip --repo /path/to/source`

配置为 `{"native":"saved.epro2","document":"pcb-uuid"}`，路径相对配置文件。
需要 Shapely 2.x。仅支持零线宽、闭合直线环的原生 FILL；保持顶点，不做简化。
使用 Gerber 4:6 mm 和明暗极性表达孔洞，按外轮廓面积排序以保留孔内铜岛。
宿主官方导出的行为不因此改变；此包不包括焊盘、走线、钻孔、阻焊或完整铜文字。
必须用独立参考几何重解析比较，导出完成本身不是通过，也不能直接作为制造包。

连接图提取可读取单位缩放、90度整数倍旋转的封装，登记焊盘中心及原生层段；
缓存铜区通过同层点包含关系接入。它不是完整的焊盘/线宽/圆弧/铜边接触求解器。
孔径和叠孔判据仍独立，模块外部网表不能推导模块内部导线；缺少内部证据时保持未完成。

## 收尾检查的统一入口

以下检查器也通过 `experiment check` 运行，并保留原始结果、代码/输入哈希与运行失败：

| checker | 输入键 | 结果边界 |
|---|---|---|
| live-contacts | dump, rules, layers, native, native-vias | 新鲜回读及原生铜接触图；保持 incomplete，不证明电路意图 |
| gerber-package | export | ZIP/哈希/文件清单检查；保持 incomplete，不代表几何通过 |
| gerber-via-drills | export, vias, rules | 过孔钻孔坐标、孔径、文件层段；不包含焊盘孔或加工验收 |
| gerbonara-parser | export | 第三方语法与宏光圈展开兼容性；依赖缺失/解析成功均不升级为整板通过 |

Gerber 回执指向的 ZIP 必须一并登记为证据。接触图需要隔离的 Shapely 2.x 环境；
Gerbonara 兼容检查计划使用 1.6.3，但未安装或未在真实包上运行时必须保持 unavailable。
网络下载失败进行有界诊断后停止，不循环尝试，不关闭 TLS 校验。
小板回归须单独声明是已有工程回归还是从需求重新设计；前者不能宣传为全新设计 E2E。

`pin-paths` 检查器输入为 graph、dump、requirements。requirements.pinGroups 显式列出
应互连的位号.脚号组，不能从待测连通图反推。通过图的实体接触边判断，不按网名连接；
图仍缺几何时即使所需路径成立也保持 incomplete；只在所声明范围内判定，不证明实物功能。

无孔焊盘的小偏移按不确定性内外包围处理，不直接忽略；带孔偏移仍保持未支持。
包含曲线铺铜的小板即使离散焊盘/走线路径成立，也不自动证明整板铜无短路。
