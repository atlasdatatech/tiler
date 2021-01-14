
# 地图下载器 Tiler - map tiles downloader

A well-polished tile downloader

一个极速地图下载框架，支持谷歌、百度、高德、天地图、Mapbox、OSM、四维、易图通等。

- 支持多任务多线程配置，可任意设置

- 支持不同层级设置不同下载范围，以加速下载

- 支持轮廓精准下载，支持轮廓裁剪

- 支持矢量瓦片数据下载

- 支持文件和MBTILES两种存储方式

- 支持自定义瓦片地址

## 使用方式

1. 下载源代码在对应的平台上自己编译

2. 直接release发布页面, 下载对应平台的预编译程序

参照配置文件中的示例url更改为想要下载的地图地址，即可启动下载任务~
> 例如: url = "http://mt0.google.com/vt/lyrs=s&x={x}&y={y}&z={z}" ,地址中瓦片的xyz使用{x}{y}{z}代替，其他保持不变。
