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
	Type        string //no types,maybe "tms" or "xyz"
	Min         int
	Max         int
	Format      TileFormat
	Host        string
	Schema      string
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
	var list = []string{"http://mt2.google.com/vt/lyrs=y&x={x}&y={y}&z={z}", "http://tile.openstreetmap.org/{z}/{x}/{y}.png", "http://api.mapbox.com/v4/mapbox.satellite/{z}/{x}/{y}.png?access_token=pk.eyJ1IjoiYWVyb3Zpc2lvbmtlc3RyZWwiLCJhIjoiY2l5bDhzYTVqMDAxNDJ3bGp1ZHA2cmtiaCJ9.8o3pqTWKiOV8RhjNGFW0rg"}
	for i, v := range list {
		fmt.Println(i, v)
		m := TileMap{}
		tml[i] = m
	}
	return tml
}

//TileURL 获取瓦片URL
func (m TileMap) getTileURL(t maptile.Tile) string {
	z := int(t.Z)
	if m.Type == "xyz" {
		z = m.Max - z
	}
	tileURL := strings.Replace(m.Schema, "{x}", strconv.Itoa(int(t.X)), -1)
	tileURL = strings.Replace(tileURL, "{y}", strconv.Itoa(int(t.Y)), -1)
	tileURL = strings.Replace(tileURL, "{z}", strconv.Itoa(z), -1)
	return m.Host + tileURL
	// return fmt.Sprintf(`http://mt1.google.cn/vt/hl=zh-CN&gl=cn&x=%d&y=%d&zoom=%d&lyrs=m`, t.X, t.Y, 17-t.Z) //google
	// return fmt.Sprintf(`http://datahive.minedata.cn/data/Buildingmore/%d/%d/%d?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873`, t.Z, t.X, t.Y)
	// return fmt.Sprintf(`http://datahive.minedata.cn/data/ResidentialPolygon/%d/%d/%d?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873`, t.Z, t.X, t.Y)
}
