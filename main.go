package main

import (
	"bytes"
	"fmt"
	"time"

	"io/ioutil"

	log "github.com/sirupsen/logrus"

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

func main() {
	cfgV = InitConf()
	minz, maxz := 10, 12
	f := loadFeature("./geojson/beijing.geojson")
	start := time.Now()
	tm := TileMap{
		Min:    0,
		Max:    17,
		Format: JPG,
		Type:   "xyz",
		Host:   "http://mt1.google.cn",
		Schema: "/vt/hl=zh-CN&gl=cn&x={x}&y={y}&zoom={z}&lyrs=m",
	}
	task := NewTask(f.Geometry, minz, maxz, tm)
	task.Download()
	secs := time.Since(start).Seconds()
	fmt.Printf("%.3fs finished...\n", secs)
}
