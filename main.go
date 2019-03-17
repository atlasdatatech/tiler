package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/shiena/ansicolor"
	log "github.com/sirupsen/logrus"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/spf13/viper"

	_ "github.com/shaxbee/go-spatialite"
)

//flag
var (
	hf bool
	cf string
)

func init() {
	flag.BoolVar(&hf, "h", false, "this help")
	flag.StringVar(&cf, "c", "conf.toml", "set config `file`")
	// 改变默认的 Usage，flag包中的Usage 其实是一个函数类型。这里是覆盖默认函数实现，具体见后面Usage部分的分析
	flag.Usage = usage
	//InitLog 初始化日志
	log.SetFormatter(&nested.Formatter{
		HideKeys:        true,
		ShowFullLevel:   true,
		TimestampFormat: "2006-01-02 15:04:05.000",
		// FieldsOrder: []string{"component", "category"},
	})
	// then wrap the log output with it
	log.SetOutput(ansicolor.NewAnsiColorWriter(os.Stdout))
	log.SetLevel(log.DebugLevel)
}
func usage() {
	fmt.Fprintf(os.Stderr, `tiler version: tiler/0.9.19
Usage: tiler [-h] [-c filename]
`)
	flag.PrintDefaults()
}

//initConf 初始化配置
func initConf(cfgFile string) {
	if _, err := os.Stat(cfgFile); os.IsNotExist(err) {
		log.Warnf("config file(%s) not exist", cfgFile)
	}
	viper.SetConfigType("toml")
	viper.SetConfigFile(cfgFile)
	viper.AutomaticEnv() // read in environment variables that match
	err := viper.ReadInConfig()
	if err != nil {
		log.Warnf("read config file(%s) error, details: %s", viper.ConfigFileUsed(), err)
	}
	viper.SetDefault("app.version", "v 0.1.0")
	viper.SetDefault("app.title", "MapCloud Tiler")
	viper.SetDefault("output.format", "mbtiles")
	viper.SetDefault("output.directory", "output")
	viper.SetDefault("task.workers", 4)
	viper.SetDefault("task.savepipe", 8)
}

func main() {
	flag.Parse()
	if hf {
		flag.Usage()
		return
	}
	if cf == "" {
		cf = "conf.toml"
	}
	initConf(cf)
	start := time.Now()
	tm := TileMap{
		Min:    viper.GetInt("tm.min"),
		Max:    viper.GetInt("tm.max"),
		Format: viper.GetString("tm.format"),
		Schema: viper.GetString("tm.schema"),
		URL:    viper.GetString("tm.url"),
		// URL:    "http://mt0.google.cn/vt/lyrs=s&hl=zh-CN&x={x}&y={y}&z={z}", ///data/landcover/{z}/{x}/{y}.pbf?key=hWWfWrAiWGtv68r8wA6D
		// URL: "http://tiles.emapgo.cn/data/emg.china-streets/{z}/{x}/{y}.pbf",
		// URL: "http://datahive.minedata.cn/data/Buildingmore/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873", //>14
		// URL: "http://datahive.minedata.cn/mergeddata/Adminflag,Annotation,Poi,Ptline,Railway,Road,Villtown,Worldannotation/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",
	}
	type cfgLayer struct {
		Min     int
		Max     int
		Geojson string
		URL     string
	}
	var cfgLrs []cfgLayer
	err := viper.UnmarshalKey("lrs", &cfgLrs)
	if err != nil {
		log.Fatal("lrs配置错误")
	}
	var layers []Layer
	for _, lrs := range cfgLrs {
		for z := lrs.Min; z <= lrs.Max; z++ {
			c := loadCollection(lrs.Geojson)
			layer := Layer{
				URL: lrs.URL,
				// URL: "http://datahive.minedata.cn/mergeddata/Adminflag,Annotation,Poi,Ptline,Railway,Road,Villtown,Worldannotation/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",
				// URL: "http://datahive.minedata.cn/data/ResidentialPolygon/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",
				// URL:        "http://tiles.emapgo.cn/data/emg.china-streets/{z}/{x}/{y}.pbf",
				Zoom:       z,
				Collection: c,
			}
			layers = append(layers, layer)
		}
	}
	task := NewTask(layers, tm)
	task.Download()
	secs := time.Since(start).Seconds()
	fmt.Printf("\n%.3fs finished...", secs)

	// for z := 0; z <= 6; z++ {
	// 	c := loadCollection("./geojson/z1-6.global.geojson")
	// 	layer := Layer{
	// 		// URL: "http://datahive.minedata.cn/mergeddata/Adminflag,Annotation,Poi,Ptline,Railway,Road,Villtown,Worldannotation/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",
	// 		URL: "http://datahive.minedata.cn/data/ResidentialPolygon/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",
	// 		// URL:        "http://tiles.emapgo.cn/data/emg.china-streets/{z}/{x}/{y}.pbf",
	// 		Zoom:       z,
	// 		Collection: c,
	// 	}
	// 	layers = append(layers, layer)
	// }

	// for z := 14; z <= 17; z++ {
	// 	c := loadCollection("./geojson/beijing.geojson")
	// 	layer := Layer{
	// 		// URL: "http://datahive.minedata.cn/mergeddata/Adminflag,Annotation,Poi,Ptline,Railway,Road,Villtown,Worldannotation/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",//merge8
	// 		// URL: "http://datahive.minedata.cn/data/Waterface/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",
	// 		// URL: "http://datahive.minedata.cn/data/Greenface/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",
	// 		// URL: "http://datahive.minedata.cn/data/Landuse/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",            //>13
	// 		// URL: "http://datahive.minedata.cn/data/Ptstop/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",             //>13
	// 		// URL: "http://datahive.minedata.cn/data/ResidentialPolygon/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873", //>13
	// 		URL: "http://datahive.minedata.cn/data/Buildingmore/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5874", //>14
	// 		// URL: "http://datahive.minedata.cn/data/Zlevel/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",             //>15
	// 		// URL: "http://datahive.minedata.cn/data/Subwaypolygon/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",      //>15
	// 		// URL: "http://datahive.minedata.cn/data/Ptexit/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",             //>16
	// 		// URL: "http://datahive.minedata.cn/data/Trafficlight/{z}/{x}/{y}?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873",       //>16
	// 		// URL:        "http://tiles.emapgo.cn/data/emg.china-streets/{z}/{x}/{y}.pbf",
	// 		Zoom:       z,
	// 		Collection: c,
	// 	}
	// 	layers = append(layers, layer)
	// }

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

}
