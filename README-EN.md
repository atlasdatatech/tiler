# Tiler - map tiles downloader
A well-polished tile downloader

Tiler is a fast map downloading framework that supports Google, Baidu, Gaode, Tiantu, Mapbox, OSM, Siwei, Yitutong, etc.
- Support multi-task and multi-thread configuration, can be set arbitrarily
- Support different download ranges at different levels to speed up downloads
- Support precise download of contours and contour cutting
- Support vector tile data download
- Support file and MBTILES two storage methods
- Support custom tile address

## How to use

1. Download the source code and compile it yourself on the corresponding platform
2. Directly release the release page, download the pre-compiled program for the corresponding platform

Refer to the example url in the configuration file and change it to the address of the map you want to download, then start the download task~

> For example: url = "http://mt0.google.com/vt/lyrs=s&x={x}&y={y}&z={z}", the xyz of the tile in the address uses {x}{y} {z} instead, the others remain unchanged.