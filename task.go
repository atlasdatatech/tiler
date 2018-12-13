package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/maptile"
	"github.com/paulmach/orb/maptile/tilecover"
	"github.com/teris-io/shortid"
)

//MBTileVersion mbtiles版本号
const MBTileVersion = "1.2"

//MetaData Mbtiles元数据
type MetaData struct {
	ID          string
	Name        string
	Basename    string
	Description string
	Attribution string
	Format      TileFormat
	Minzoom     int
	Maxzoom     int
	Size        int
	Mtime       time.Time
	Version     string
	Count       int64
	JSON        string
	Type        string
}

//TileFormat 获取瓦片格式
func (metaData MetaData) TileFormat() TileFormat {
	return metaData.Format
}

//MetaItems 输出
func (metaData MetaData) MetaItems() map[string]string {
	data := map[string]string{
		"name":        metaData.Name,
		"description": metaData.Description,
		"format":      metaData.Format.String(),
		"version":     metaData.Version,
		"type":        metaData.Type,
		// "bounds":      metaData.bounds,
		// "minzoom": strcon.Atoi(metaData.Minzoom),
		// "maxzoom": metaData.Maxzoom,
	}
	return data
}

//Task 下载任务
type Task struct {
	ID          string
	Name        string
	Description string
	File        string
	Progress    string
	Total       int64
	Max         int
	Min         int
	TileMap     TileMap
	Geom        orb.Geometry
	Levels      Zoom
	MetaData    MetaData
	db          *sql.DB
}

//NewTask 创建下载任务
func NewTask(geom orb.Geometry, minzoom, maxzoom int, m TileMap) *Task {
	id, _ := shortid.Generate()

	task := Task{
		ID:      id,
		Name:    id,
		Geom:    geom,
		TileMap: m,
		Max:     maxzoom,
		Min:     minzoom,
	}

	for z := minzoom; z <= maxzoom; z++ {
		count := tilecover.GeometryCount(geom, maptile.Zoom(z))
		zoom := ZoomCount{
			Level: z,
			Count: count,
		}
		task.Total += count
		task.Levels = append(task.Levels, zoom)
	}

	task.GenerateMetaData()

	return &task
}

//GenerateMetaData 生成MetaData
func (task Task) GenerateMetaData() error {
	// task.MetaData
	return nil
}

func (task Task) setupMBTileTables() error {

	if task.File == "" {
		outdir := cfgV.GetString("output.directory")
		os.MkdirAll(outdir, os.ModePerm)
		task.File = filepath.Join(outdir, task.ID+".mbtiles")
	}
	os.Remove(task.File)
	db, err := sql.Open("sqlite3", task.File)
	if err != nil {
		return err
	}

	err = optimizeConnection(db)
	if err != nil {
		return err
	}

	if task.db == nil {
		return fmt.Errorf(`mbtile database conn is nil, must prepare database first`)
	}

	_, err = task.db.Exec("create table if not exists tiles (zoom_level integer, tile_column integer, tile_row integer, tile_data blob);")
	if err != nil {
		return err
	}

	_, err = task.db.Exec("create table if not exists metadata (name text, value text);")
	if err != nil {
		return err
	}

	_, err = task.db.Exec("create unique index name on metadata (name);")
	if err != nil {
		return err
	}

	_, err = db.Exec("create unique index tile_index on tiles(zoom_level, tile_column, tile_row);")
	if err != nil {
		return err
	}

	// Load metadata.
	for name, value := range task.MetaData.MetaItems() {
		_, err := db.Exec("insert into metadata (name, value) values (?, ?)", name, value)
		if err != nil {
			return err
		}
	}

	task.db = db //保存任务的库连接
	return nil
}

//SaveTile 保存瓦片
func (task Task) SaveTile(tile Tile) error {
	t := "mbtiles" //or files
	switch t {
	case "mbtiles":
		saveToMBTile(tile, task.db)
	case "files":
	}

	return nil
}
