# EasyEDA 修改版

本项目基于 [zhoushoujianwork/easyeda-agent](https://github.com/zhoushoujianwork/easyeda-agent)，为嘉立创 EDA 专业版增加实验记录、物理连接核验和制造文件检查。仓库名称为 `easyeda-modified`。

## 运行

安装 Go 1.26.3 或兼容版本后，在源码根目录运行：

```sh
go build -o easyeda ./cmd/easyeda
./easyeda --help
./easyeda experiment --help
```

实验检查需要 Python 3.9 或更新版本，几何检查还需要 Shapely 2.x。操作实际电路时，请按[连接器与本地服务配置说明](.agents/skills/easyeda-agent/references/environment-setup.md)连接嘉立创 EDA 专业版 Web 编辑器。Vercel 页面提供公开的项目说明，编辑和回读操作在本地执行。

## 已验证的范围

修改版可记录检查输入和结果，核对支持的焊盘、走线及过孔几何，检查指定引脚之间是否有物理路径。在已有的分压电路回归中，三组指定路径均成立，PCB 规则检查返回零违规，并取得生产文件包。曲线铺铜、原理图现场复核、文档重开和完整制造文件验收仍有缺口。详见[实验检查器使用说明](.agents/skills/easyeda-agent/references/experiment-records.md)。

## 许可与署名

上游作者和第三方组件声明见 [LICENSE](LICENSE)、[NOTICE](NOTICE) 和 [Apache 2.0 组件许可](extension/src/beautify/LICENSE)。
