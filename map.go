package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/paulmach/orb/maptile"
)

//TileMap 瓦片地图类型
type TileMap struct {
	ID          int
	Name        string
	Description string
	Schema      string //no types,maybe "xyz" or "tms"
	Min         int
	Max         int
	Format      TileFormat
	URL         string
	Token       string
	//such as porxy...
}

//CreateTileMap 添加地图
func CreateTileMap(url string) {
	// tileMap := TileMap{}
	//tileMap.Save()
	//成功默认保存到数据库
}

//GetTileMapList 获取初始化默认地图列表
func GetTileMapList() map[int]TileMap {
	tml := make(map[int]TileMap)
	var list = []string{"http://mt2.google.com/vt/lyrs=y&x={x}&y={y}&z={z}",
		"http://tile.openstreetmap.org/{z}/{x}/{y}.png",
		"http://api.mapbox.com/v4/mapbox.satellite/{z}/{x}/{y}.png?access_token=pk.eyJ1IjoiYWVyb3Zpc2lvbmtlc3RyZWwiLCJhIjoiY2l5bDhzYTVqMDAxNDJ3bGp1ZHA2cmtiaCJ9.8o3pqTWKiOV8RhjNGFW0rg",
		// "http://mt0.google.cn/vt/lyrs=s&hl=zh-CN&x=214130&y=114212&z=18",
		"http://mt0.google.cn/vt/lyrs=y&hl=zh-CN&x={x}&y={y}&z={z}",
		"http://mt0.google.cn/vt/lyrs=s&x={x}&y={y}&z={z}",
		"http://mt2.google.cn/vt/lyrs=y@258000000&hl=zh-CN&gl=CN&src=app&x=214130&y=114212&z=18&s=Ga", //m：路线图,t：地形图,p：带标签的地形图,s：卫星图,y：带标签的卫星图,h：标签层（路名、地名等）
	}
	for i, v := range list {
		fmt.Println(i, v)
		m := TileMap{}
		tml[i] = m
	}
	return tml
}

//TileURL 获取瓦片URL
func (m TileMap) getTileURL(t maptile.Tile) string {
	url := strings.Replace(m.URL, "{x}", strconv.Itoa(int(t.X)), -1)
	url = strings.Replace(url, "{y}", strconv.Itoa(int(t.Y)), -1)
	url = strings.Replace(url, "{z}", strconv.Itoa(int(t.Z)), -1)
	return url
	// return fmt.Sprintf(`http://mt1.google.cn/vt/hl=zh-CN&gl=cn&x=%d&y=%d&zoom=%d&lyrs=m`, t.X, t.Y, 17-t.Z) //google
	// return fmt.Sprintf(`http://datahive.minedata.cn/data/Buildingmore/%d/%d/%d?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873`, t.Z, t.X, t.Y)
	// return fmt.Sprintf(`http://datahive.minedata.cn/data/ResidentialPolygon/%d/%d/%d?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873`, t.Z, t.X, t.Y)
}
