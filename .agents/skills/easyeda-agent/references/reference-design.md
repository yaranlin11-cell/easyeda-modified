# 参考辅助设计：来源、适用范围和测试链

用于用户提供人工原理图、PCB、制造规格并要求复现或改进工作流的任务。
参考成果不是无条件正确的答案；先核对版本、功能与作用对象，再比较设计。

## 1. 输入与任务模式

在 S0 源数据旁保存 `reference.json`，通过 `spec reference-check --from reference.json`
核对。它是来源/证据清单，不替代 S0 功能模块、canonical pin→net 数据或执行队列。

- `demo`：用户授权补充示例；报告明确仅验证示例，不外推为原产品完成。
- `reference-assisted`：人工成果辅助复现；不得计为盲测或独立设计成功。
- `independent`：依据需求独立生成；比较用的参考答案不能作为生成器输入。

只有制造参数时，不能推导出完整电路。缺失功能、引脚、接口或链路定义，应记录未决项；
不能自动换成简单电路，并以该电路 DRC 通过代表目标任务通过。
模块、载板、装配体分别声明 scope；文档明确的作用对象优先，不因文件名或经验擅自套用。

## 2. 来源清单契约（schemaVersion 1）

```json
{
  "schemaVersion": 1,
  "mode": "reference-assisted",
  "scopes": ["module", "carrier", "assembly"],
  "sources": [{
    "id": "schematic", "path": "reference.pdf", "sha256": "原文件64位SHA256",
    "role": "schematic-reference", "scope": "assembly", "revision": "1.0",
    "readStatus": "partial"
  }],
  "requirements": [{
    "id": "chain-A", "scope": "assembly", "kind": "topology",
    "statement": "链路入口、出口和待测焊点须逐项确认",
    "evidence": [{"sourceId": "schematic", "locator": "page 2, chain A"}],
    "resolution": "unresolved"
  }],
  "capabilities": [{
    "id": "physical-graph-extraction", "required": true,
    "status": "unknown", "evidence": ""
  }],
  "unresolved": [{"id": "pin-table", "detail": "完整引脚连接表待核对", "blocking": true}]
}
```

- `sources.role`：requirements / manufacturing / schematic-reference / pcb-reference。
- `readStatus`：complete / partial / unreadable；哈希绑定文件字节，不代表内容已正确理解。
- `requirements.kind`：functional / electrical / mechanical / manufacturing / topology。
- `resolution`：verified / assumed / unresolved；假设不能静默升级为已确认事实。
- 能力状态：live-verified / offline-verified / unsupported / unknown。必需能力没有现场证据，
  输入就绪报告保持 incomplete；软件测试通过不证明该宿主已经支持此操作。
- 来源路径相对 manifest 解析。未知字段、错误枚举、重复 ID、来源哈希不符返回 fail。
- partial、必需能力不足、阻塞问题返回 incomplete，两者进程码均非零。pass 只表示清单完整且
  文件哈希匹配；resolution/capability 仍是调用方声明，不是自动验证了电路或签发操作许可。

## 3. 文件解析与版本对账

先运行 `spec source-probe <file>`。零退出只表示探测执行成功；读取 extractionStatus 和
limitations。PDF 字体乱码必须结合渲染核对；引脚、分支、跨页标签不能仅靠 OCR 自动确认。

Allegro 原生二进制可能内嵌 ZIP/JSON 预览；可见板框、文字、材料库名称不代表完整几何、
实例、网络、过孔或实际层叠。探测器不解压文件到其内嵌路径，也不自动上传原文件。
使用经验证的转换/导出通道，再核对元件、引脚、焊盘、网表、层、孔、单位及坐标原点。
没有完整导出时暂停依赖数据的布局复现；仍可整理原理图需求、开发离线校验和列出缺口。

PDF 标题栏、修订记录、制造规格日期、PCB 元数据和库名称分别记录；文件名日期或封装名中的
旧版本只能作为冲突线索，不能自行认定已升级、完全一致或错误。冲突先定范围再处理。

## 4. 菊花链物理图检查

```bash
easyeda spec chain-check --from chains.json --observed graph.json
```

`chains.json` 独立声明要求，不从待检查结果反推：

```json
{"schemaVersion":1,"chains":[
  {"id":"A","orderedNodes":["J.1","PCB.P1","MODULE.P1","MODULE.P2","PCB.P2","J.2"],
   "requiredJoints":["solder-1","solder-2"]}
]}
```

`graph.json` 使用以下字段（均为数据，不是执行脚本）：

- schemaVersion: 1；complete: 布尔完整性声明。
- coverage: 必须包含 board-copper、module-internal、joints。
- sources: `[{id,path,sha256}]`，每个实际输入文件都绑定字节哈希。
- nodes: `[{id,scope}]`，scope 为 board/module，ID 是物理端点，不能用网名合并待测焊点两端。
- edges: `[{id,from,to,kind,evidence}]`，kind 为 board-copper/module-internal/joint；
  evidence 引用 sources.id。载板铜仅连接 board 节点，模块内部边仅连接 module 节点，joint 跨两者。

检查断路、必经节点顺序、节点/焊点旁路及不同链之间的串接。移除任一 requiredJoint 后，
入口到出口必须断开；未断开就是存在旁路。允许不旁路关键节点的末端测试点支路。
当前命令假定所有声明链彼此隔离，仅适用于静态无向导通关系，不处理开关状态或器件阻抗。
图中平行边必须保留，否则可能掩盖焊点旁路。

必须覆盖铺铜、填充、过孔、封装内部铜和其他可能形成捷径的连接；只采集 track-list 不足。
模块内部配对未知时返回 incomplete，不能把所有同名 GND 脚直接短接，也不能视为 NC。
该命令没有自动 PCB→完整物理图适配器；只能验证提供的图。来源哈希不能证明图提取正确，
调用方须独立核对节点/边与原始来源，报告保留此限制，不将离线 PASS 写成 live-verified。

## 5. 执行与验收

原有 compose/apply、原生 DRC 和 save→reload→fresh readback 仍然执行。
需求覆盖、实际 pin→pad→net、菊花链拓扑、机械、制造参数及图面各自给出证据。
人工板与自动板可以布局不同；要求相同的功能/焊点链不可丢，未知不能以相似度或 DRC 掩盖。

回归至少包含：正确链、断路、绕过焊点、并行旁路、错误顺序、跨链短接、合法测试点支路、
未读取模块、缺铜类型、来源文件变更、模块/载板范围混用、制造参数被误当完整电路。
使用合成公开测试；客户 PDF、PCB、引脚答案与完整日志留在 artifacts 或仓库外。

## 6. 数值制造参数比较

```bash
easyeda spec constraints-check --from constraints.json --observed measurements.json
```

两份文件均为 schemaVersion 1，sources 为 `[{id,path,sha256}]`，路径相对各自 JSON。
要求文件使用 rules 数组，每条包含 id、scope、subject、metric、unit、stage、mode、evidence。
观测文件使用 measurements 数组，每条包含 ruleId、scope、subject、metric、unit、stage、value、evidence。
evidence 均为 `{sourceId,locator}`，sourceId 引用该文件自己的 sources。

- mode=min：只填 min；max：只填 max；range：同时填 min/max（含边界）；reference：不填边界，不参与合格判定。参考标称值与范围保留在来源定位或需求说明中。
- stage=design 为设计数据，fabricated 为成品实测；二者必须严格匹配。
- scope 是作用对象（例如 carrier/module）；subject 是明确对象或集合统计量；metric 是测量属性。三者均须匹配。
- unit 支持 mm、um、mil、percent、count、C；长度可自动换算，其他维度不互换。count 必须为非负整数，长度非负，percent 为 0–100，温度不低于绝对零度。
- 缺测量或 value=null 为 incomplete；未知字段、重复/未知规则、来源改变、单位或范围不符为 fail。
- 只有参考项或空要求不能得到 pass。reference 项缺观测不阻塞，但有观测时仍需正确来源与单位。

例如要求条目（其来源 s 必须在 sources 中绑定真实文件）：

```json
{"id":"through-hole-min","scope":"carrier","subject":"minimum-through-via-hole",
 "metric":"diameter","unit":"um","stage":"design","mode":"min","min":200,
 "evidence":{"sourceId":"s","locator":"via table, minimum hole diameter"}}
```

对应独立观测 0.25 mm 满足此最小值；不得因不等于 0.20 mm 而报错。
同理，±容差须先转换为明确上下界，不能自动把最小值转换为等值。
若要求所有焊盘处于某范围，必须验证完整对象集合的最小值与最大值，不能只测一只焊盘或平均值。
本命令不验证集合覆盖或提取算法；每条聚合观测须保存原始明细与提取方法，并单独对账数量。
没有实际采集时保持 measurements=[]，不要把 PDF 要求复制成观测值。

该命令是离线数值检查，尚无自动 EDA/工厂测量适配器。层序、材料牌号、过孔层对、
填孔工艺、pin→pad 映射仍需要独立核对；所有数值通过也不构成整板验收。

## 7. 过孔和层叠的实际接口边界

当前连接器 `pcb.via.create` 仅实现默认通孔，payload 允许 x/y/net/holeDiameter/diameter。
未知字段（包括 viaType、designRuleBlindViaName、startLayer/endLayer、填孔参数）在写入前拒绝，
不得删除这些要求后重试并声称满足原要求。官方类型声明虽包含过孔类型与规则名，当前工作流
仍缺规则表完整配置、层对解析及现场回读证据，不能直接升级为盲埋孔支持。

`pcb.via.list` 增加 viaType、designRuleBlindViaName、viaMetadataAvailable。宿主缺少 getter 时
返回 null/false，不据此假设通孔；规则名不是解析后的开始层/结束层。消费端必须继续核对层对。

`pcb.stackup.set` 仅接受 count 与 layers；各层只接受 id/layer、type、name。
材料、厚度等未实现字段直接拒绝；重复/冲突层 ID、错误类型、空层条目在任何层数写入前拒绝。
此检查不保证宿主最终接受所有合法请求；仍须检查 changed/partial/verified 和保存重载回读。
没有连接窗口时不得称新连接器已热加载或现场验证成功。
