package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/paulmach/orb/maptile"

	"io/ioutil"

	"github.com/paulmach/orb/encoding/mvt"
	"github.com/paulmach/orb/geojson"
	"github.com/paulmach/orb/maptile/tilecover"
	"github.com/paulmach/orb/simplify"
	log "github.com/sirupsen/logrus"
	pb "gopkg.in/cheggaaa/pb.v1"

	"github.com/spf13/viper"

	_ "github.com/shaxbee/go-spatialite"
)

//InitConf 用来设定初始配置
func InitConf() *viper.Viper {
	v := viper.New()
	//使用 toml 的格式配置文件
	v.SetConfigType("toml")
	buf, err := ioutil.ReadFile("tiler.toml")
	if err != nil {
		log.Fatal("read config file error:" + err.Error())
		return v
	}
	err = v.ReadConfig(bytes.NewBuffer(buf))
	if err != nil {
		log.Fatal("config file has error:" + err.Error())
		return v
	}
	//配置默认值，如果配置内容中没有指定，就使用以下值来作为配置值，给定默认值是一个让程序更健壮的办法
	v.SetDefault("app.title", "MapCloud Tiler")
	return v
}

var cfgV *viper.Viper

func orb2mvt() {
	buf, err := ioutil.ReadFile("./geojson/z1-6.global.geojson")
	if err != nil {
		log.Fatalf("unable to read file: %v", err)
	}
	fc, err := geojson.UnmarshalFeatureCollection(buf)
	if err != nil {
		log.Fatalf("unable to unmarshal feature: %v", err)
	}
	file := filepath.Join("vtiler_output", "out.mbtiles")
	os.Remove(file)
	db, err := sql.Open("sqlite3", file)
	if err != nil {
		log.Fatalf("unable to read file: %v", err)
	}
	_, err = db.Exec("create table if not exists tiles (zoom_level integer, tile_column integer, tile_row integer, tile_data blob);")
	if err != nil {
		log.Fatalf("unable to read file: %v", err)
	}

	_, err = db.Exec("create table if not exists metadata (name text, value text);")
	if err != nil {
		log.Fatalf("unable to read file: %v", err)
	}

	_, err = db.Exec("create unique index name on metadata (name);")
	if err != nil {
		log.Fatalf("unable to read file: %v", err)
	}

	_, err = db.Exec("create unique index tile_index on tiles(zoom_level, tile_column, tile_row);")
	if err != nil {
		log.Fatalf("unable to read file: %v", err)
	}
	// Start with a set of feature collections defining each layer in lon/lat (WGS84).
	collections := map[string]*geojson.FeatureCollection{}
	collections["newlayer"] = fc
	count := 0
	for z := 0; z <= 6; z++ {
		cnt := tilecover.BoundCount(fc.Features[0].Geometry.Bound(), maptile.Zoom(z))
		count += int(cnt)
	}
	bar := pb.New(count).Prefix(fmt.Sprintf("Zoom %d : ", 6)).Postfix("\n")
	bar.Start()
	for z := 0; z <= 6; z++ {
		set := tilecover.Bound(fc.Features[0].Geometry.Bound(), maptile.Zoom(z))
		for tile := range set {
			bar.Increment()
			// Convert to a layers object and project to tile coordinates.
			layers := mvt.NewLayers(collections)
			layers.ProjectToTile(tile)
			// In order to be used as source for MapboxGL geometries need to be clipped
			// to max allowed extent. (uncomment next line)
			layers.Clip(mvt.MapboxGLDefaultExtentBound)
			// Simplify the geometry now that it's in the tile coordinate space.
			layers.Simplify(simplify.DouglasPeucker(1.0))
			// Depending on use-case remove empty geometry, those two small to be
			// represented in this tile space.
			// In this case lines shorter than 1, and areas smaller than 1.
			layers.RemoveEmpty(1.0, 1.0)
			// Sometimes MVT data is stored and transfered gzip compressed. In that case:
			data, err := mvt.MarshalGzipped(layers)
			_, err = db.Exec("insert into tiles (zoom_level, tile_column, tile_row, tile_data) values (?, ?, ?, ?);", tile.Z, tile.X, tile.Y, data)
			if err != nil {
				fmt.Printf("%v", err)
			} else {
				fmt.Printf("saved, z:%d, x:%d, y:%d\n", tile.Z, tile.X, tile.Y)
			}
		}
	}
	db.Close()
	bar.FinishPrint(fmt.Sprintf("zoom %d finished ~", 6))
}

func main() {
	cfgV = InitConf()
	min, max := 0, 14
	start := time.Now()
	tm := TileMap{
		Min:    min,
		Max:    max,
		Format: PBF,
		Schema: "xyz",
		// URL:    "http://mt0.google.cn/vt/lyrs=s&hl=zh-CN&x={x}&y={y}&z={z}", ///data/landcover/{z}/{x}/{y}.pbf?key=hWWfWrAiWGtv68r8wA6D
		URL: "http://tiles.emapgo.cn/data/emg.china-streets/{z}/{x}/{y}.pbf",
	}

	var layers []Layer
	for z := 0; z <= 7; z++ {
		c := loadCollection("./geojson/z1-6.global.geojson")
		layer := Layer{
			URL:        "http://tiles.emapgo.cn/data/emg.china-streets/{z}/{x}/{y}.pbf",
			Zoom:       z,
			Collection: c,
		}
		layers = append(layers, layer)
	}

	for z := 8; z <= 14; z++ {
		c := loadCollection("./geojson/z7-10.china.geojson")
		layer := Layer{
			URL:        "http://tiles.emapgo.cn/data/emg.china-streets/{z}/{x}/{y}.pbf",
			Zoom:       z,
			Collection: c,
		}
		layers = append(layers, layer)
	}

	// for z := 11; z <= 13; z++ {
	// 	c := loadCollection("./geojson/z11-13.gansu.geojson")
	// 	layer := Layer{
	// 		URL:        "http://mt0.google.cn/vt/lyrs=s&x={x}&y={y}&z={z}",
	// 		Zoom:       z,
	// 		Collection: c,
	// 	}
	// 	layers = append(layers, layer)
	// }

	// for z := 14; z <= 16; z++ {
	// 	c := loadCollection("./geojson/z14-16.lanzhou.geojson")
	// 	layer := Layer{
	// 		URL:        "http://mt0.google.cn/vt/lyrs=s&x={x}&y={y}&z={z}",
	// 		Zoom:       z,
	// 		Collection: c,
	// 	}
	// 	layers = append(layers, layer)
	// }
	// for z := 17; z <= 18; z++ {
	// 	c := loadCollection("./geojson/z17-18.lanzhou.geojson")
	// 	layer := Layer{
	// 		URL:        "http://mt0.google.cn/vt/lyrs=s&x={x}&y={y}&z={z}",
	// 		Zoom:       z,
	// 		Collection: c,
	// 	}
	// 	layers = append(layers, layer)
	// }
	task := NewTask(layers, tm)
	task.Download()
	secs := time.Since(start).Seconds()
	fmt.Printf("\n%.3fs finished...", secs)
}
