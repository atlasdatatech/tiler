package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/shiena/ansicolor"
	log "github.com/sirupsen/logrus"

	nested "github.com/antonfisher/nested-logrus-formatter"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/viper"
)

// flag
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
	fmt.Fprintf(os.Stderr, `tiler version: tiler/v0.1.0
Usage: tiler [-h] [-c filename]
`)
	flag.PrintDefaults()
}

// initConf 初始化配置
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
	viper.SetDefault("task.savepipe", 1)
	viper.SetDefault("task.timedelay", 0)
}

type TileData struct {
	Z    int
	X    int
	Y    int
	Flag bool
}

// 插入瓦片数据
func insertTiles(db *sql.Tx, tiles []TileData) error {
	// 准备插入语句
	stmt, err := db.Prepare("INSERT INTO tiles (z, x, y,flag) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	// 执行插入
	for _, tile := range tiles {
		_, err = stmt.Exec(tile.Z, tile.X, tile.Y, tile.Flag)
		if err != nil {
			return err
		}
	}
	return nil
}

func testDbTask() {

	db, err := sql.Open("sqlite3", "./tiles.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	createTableSQL := `CREATE TABLE IF NOT EXISTS tiles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		z INTEGER,
		x INTEGER,
		y INTEGER,
		flag BOOLEAN
	);`

	_, err = db.Exec(createTableSQL)
	if err != nil {
		log.Fatal(err)
	}

	// 开始事务
	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback() // 如果提交事务前发生错误，则回滚事务

	batchSize := 1 << 10
	// 插入数据
	tileBatch := make([]TileData, 0, batchSize)
	total := 0
	i := 0
	for z := 0; z <= 12; z++ {
		numTiles := 1 << uint(z) // 计算每个缩放级别的瓦片数量
		total += numTiles * numTiles
		log.Printf("级别%d,瓦片数量：%d\n", z, numTiles*numTiles)
		log.Printf("总瓦片数量：%d\n", total)
		for x := 0; x < numTiles; x++ {
			for y := 0; y < numTiles; y++ {
				tile := TileData{Z: z, X: x, Y: y, Flag: false}
				tileBatch = append(tileBatch, tile)
				// 批量插入
				if len(tileBatch) >= batchSize {
					err := insertTiles(tx, tileBatch) // 使用事务执行批量插入操作
					if err != nil {
						log.Fatal(err)
					}
					tileBatch = nil // 清空批次
				}
				i++
				if i > 99999999 {
					goto last
				}
			}

		}
	}
last:
	// 处理剩余的批次
	if len(tileBatch) > 0 {
		err := insertTiles(tx, tileBatch) // 使用事务执行批量插入操作
		if err != nil {
			log.Fatal(err)
		}
	}
	// 提交事务
	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}
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
		Name:   viper.GetString("tm.name"),
		Min:    viper.GetInt("tm.min"),
		Max:    viper.GetInt("tm.max"),
		Format: viper.GetString("tm.format"),
		Schema: viper.GetString("tm.schema"),
		JSON:   viper.GetString("tm.json"),
		URL:    viper.GetString("tm.url"),
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
				URL:        lrs.URL,
				Zoom:       z,
				Collection: c,
			}
			layers = append(layers, layer)
		}
	}
	task := NewTask(layers, tm)
	fmt.Println(task.workerCount)
	task.Download()
	secs := time.Since(start).Seconds()
	log.Printf("\n%.3fs finished...", secs)
}
