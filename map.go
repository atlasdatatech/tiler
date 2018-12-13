package main

import "fmt"

//TileMap 瓦片地图类型
type TileMap struct {
	ID          int
	Name        string
	Description string
	Type        string //no types,maybe "tms" or "xyz"
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
func (m TileMap) TileURL() string {
	return m.Host + m.Schema
}
