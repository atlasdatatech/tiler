package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"encoding/json"
	"io/ioutil"
	"net/http"
	"path/filepath"

	log "github.com/sirupsen/logrus"

	"github.com/paulmach/orb"
	"github.com/spf13/viper"
	pb "gopkg.in/cheggaaa/pb.v1"

	"github.com/paulmach/orb/maptile"

	"github.com/paulmach/orb/geojson"
	"github.com/paulmach/orb/maptile/tilecover"
	_ "github.com/shaxbee/go-spatialite"
)

var cfgV *viper.Viper

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

//Set a safety set
type Set struct {
	sync.RWMutex
	M maptile.Set
}

// tokens is a counting semaphore used to
// enforce a limit of 16 concurrent requests.
var wg sync.WaitGroup

var abort = make(chan struct{})
var pause = make(chan struct{})
var play = make(chan struct{})

func abortFun() {
	// os.Stdin.Read(make([]byte, 1)) // read a single byte
	<-time.After(8 * time.Second)
	abort <- struct{}{}
}

func pauseFun() {
	// os.Stdin.Read(make([]byte, 1)) // read a single byte
	<-time.After(3 * time.Second)
	pause <- struct{}{}
}

func playFun() {
	// os.Stdin.Read(make([]byte, 1)) // read a single byte
	<-time.After(5 * time.Second)
	play <- struct{}{}
}

const workerCount = 32
const bufferSize = 64
const failSize = 64
const mergeSize = 256

var workers = make(chan maptile.Tile, workerCount)
var fails = make(chan maptile.Tile, failSize)
var tiles = make(chan maptile.Tile, mergeSize)

var allMergedTiles = Set{M: make(maptile.Set)}

var gTotal int64
var gZoom int
var gBar *pb.ProgressBar

func getTileURL(t maptile.Tile) string {
	return fmt.Sprintf(`http://mt1.google.cn/vt/hl=zh-CN&gl=cn&x=%d&y=%d&zoom=%d&lyrs=m`, t.X, t.Y, 17-t.Z) //google
	// return fmt.Sprintf(`http://datahive.minedata.cn/data/Buildingmore/%d/%d/%d?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873`, t.Z, t.X, t.Y)
	// return fmt.Sprintf(`http://datahive.minedata.cn/data/ResidentialPolygon/%d/%d/%d?token=f7bf94956c3d4693bab79b5a63498f61&solu=5873`, t.Z, t.X, t.Y)
	// tileUrl = "http://mt2.google.com/vt/lyrs=y&x={x}&y={y}&z={z}"
	// tileURL := strings.Replace(urlSchema, "{x}", strconv.Itoa(int(t.X)), -1)
	// tileURL = strings.Replace(tileURL, "{y}", strconv.Itoa(int(t.Y)), -1)
	// tileURL = strings.Replace(tileURL, "{z}", strconv.Itoa(int(t.Z)), -1)
	// return tileURL
}

func procFails(urls []string) {
	defer wg.Done()
	for _, url := range urls {
		fmt.Printf("\n*>*>*> saving failed tiles:%s", url)
	}
}

func procTiles(ts []maptile.Tile) {
	defer wg.Done()
	//merge up append set
	gBar.Add(len(ts))
	fmt.Printf("\nmerge up append set, %d...", len(ts))
	allMergedTiles.Lock()
	allMergedTiles.M = tilecover.MergeUpAppend(allMergedTiles.M, ts, 0)
	allMergedTiles.Unlock()
}

func tileFetcher(t maptile.Tile) {
	defer wg.Done()
	defer func() {
		<-workers
	}()
	fail := func(t maptile.Tile) {
		select {
		case fails <- t:
		default: //fails full
			log.Println("process failed tiles...")
			var urls []string
			for i := 0; i < failSize; i++ {
				url := getTileURL(<-fails)
				urls = append(urls, url)
			}
			wg.Add(1)
			go procFails(urls)
		}
	}
	// start := time.Now()
	url := getTileURL(t)
	resp, err := http.Get(url)
	if err != nil {
		log.Println("http.get error in fetching tile->", err)
		fail(t)
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("read tile content error->", err)
	}
	if len(body) == 0 {
		return //zero byte tiles n
	}

	outdir := cfgV.GetString("output.directory")
	dir := filepath.Join(outdir, fmt.Sprintf(`%d`, t.Z), fmt.Sprintf(`%d`, t.X))
	os.MkdirAll(dir, os.ModePerm)
	fileName := filepath.Join(dir, fmt.Sprintf(`%d`, t.Y))
	err = ioutil.WriteFile(fileName, body, os.ModePerm)
	if err != nil {
		log.Println("create path file error->", err)
		fail(t)
		return
	}
	// secs := time.Since(start).Seconds()
	// fmt.Printf("\n%.3fs  %7d  %s", secs, len(body), url)
	select {
	case tiles <- t:
	default: //tiles full
		var ts []maptile.Tile
		for i := 0; i < mergeSize; i++ {
			ts = append(ts, <-tiles)
		}
		wg.Add(1)
		go procTiles(ts)
	}
}

func downloadMinmal(g orb.Geometry, minz int, maxz int) {

	for z := minz; z <= maxz; z++ {
		total := tilecover.GeometryCount(g, maptile.Zoom(z))
		bar := pb.New64(total).Prefix(fmt.Sprintf("\nZoom %d : ", z))
		bar.Start()
		fmt.Println("downloading started, Zoom:", z, "Tiles:", total)

		var tilelist = make(chan maptile.Tile, 128)
		go tilecover.GeometryChannel(g, maptile.Zoom(z), tilelist)

		for tile := range tilelist {
			select {
			case workers <- tile:
				bar.Increment()
				wg.Add(1)
				go tileFetcher(tile)
			case <-abort:
				close(tilelist)
				fmt.Println("Downloader got canceled!")
			case <-pause:
				fmt.Println("pause")
				select {
				case <-play:
					fmt.Println("play")
				case <-abort:
					close(tilelist)
					fmt.Println("Downloader got canceled!")
				}
			}
		}

		wg.Wait()
		bar.FinishPrint("The End!")

		var urls []string
		for l := len(fails); l > 0; l-- {
			urls = append(urls, getTileURL(<-fails))
		}
		wg.Add(1)
		go procFails(urls)

		var ts []maptile.Tile
		for l := len(tiles); l > 0; l-- {
			ts = append(ts, <-tiles)
		}
		wg.Add(1)
		go procTiles(ts)
		wg.Wait()
		// proc all none zero tiles
		log.Printf("zoom:%d,total none zero tiles:%d\n", z, len(allMergedTiles.M))
		// <-time.After(3 * time.Second)
	}
}

func downloadZoom(g orb.Geometry, zoom int) {
	z := maptile.Zoom(zoom)
	var tilelist = make(chan maptile.Tile, bufferSize)
	go tilecover.GeometryChannel(g, z, tilelist)
	for tile := range tilelist {
		select {
		case workers <- tile:
			gBar.Increment()
			wg.Add(1)
			go tileFetcher(tile)
		case <-abort:
			close(tilelist)
			fmt.Println("Downloader got canceled!")
		case <-pause:
			fmt.Println("pause")
			select {
			case <-play:
				fmt.Println("play")
			case <-abort:
				close(tilelist)
				fmt.Println("Downloader got canceled!")
			}
		}
	}

	wg.Wait()

	var urls []string
	for l := len(fails); l > 0; l-- {
		urls = append(urls, getTileURL(<-fails))
	}
	wg.Add(1)
	go procFails(urls)

	var ts []maptile.Tile
	for l := len(tiles); l > 0; l-- {
		ts = append(ts, <-tiles)
	}
	wg.Add(1)
	go procTiles(ts)
	wg.Wait()
	// proc all none zero tiles
	// log.Printf("zoom:%d,total none zero tiles:%d\n", z, len(allMergedTiles.M))
}

func downloadDepth(g orb.Geometry, minz int, maxz int) {

	total := tilecover.GeometryCount(g, maptile.Zoom(minz))
	gBar = pb.New64(total).Prefix("\n Fetching -> ")
	gBar.Start()
	downloadZoom(g, minz)
	gBar.FinishPrint("The End!")

	minz++
	for minz <= maxz {

		allMergedTiles.Lock()
		zoomSet := allMergedTiles.M
		mfc := allMergedTiles.M.ToFeatureCollection()
		ifile := len(allMergedTiles.M)
		fmt.Printf("merge up append set...%d\n", ifile)
		fmt.Println("downloading started, Zoom:", minz, "Tiles:", ifile)
		bar := pb.StartNew(ifile).Prefix(fmt.Sprintf("\nZoom %d : ", minz))
		wg.Add(1)
		go func(name string, mfc *geojson.FeatureCollection) {
			defer wg.Done()
			data, err := json.MarshalIndent(mfc, "", " ")
			if err != nil {
				log.Printf("error marshalling json: %v\n", err)
				return
			}

			err = ioutil.WriteFile(name+".geojson", data, 0644)
			if err != nil {
				log.Printf("write file failure: %v\n", err)
			}
			log.Printf("output finished : %s.geojson\n", name)

		}(strconv.FormatInt(int64(ifile), 10), mfc)

		allMergedTiles.M = make(maptile.Set)
		allMergedTiles.Unlock()
		cliperBuffer := make(chan orb.Geometry, 16)
		go func(set maptile.Set, buffer chan<- orb.Geometry) {
			defer close(buffer)
			for t := range set {
				bar.Increment()
				buffer <- t.Bound()
				// log.Println("starting cliper...")
				// start := time.Now()
				// clipped := clip.Geometry(t.Bound(), g)
				// secs := time.Since(start).Seconds()
				// if clipped != nil {
				// 	buffer <- clipped
				// 	log.Printf("cliper add to buffer,time:%.4fs...\n", secs)
				// }
			}
			log.Printf("cliper buffer closing...\n")
			// close(buffer)
		}(zoomSet, cliperBuffer)
		// need synchronous 1,2,3...
		i := 0
		for geom := range cliperBuffer {
			// j, err := geojson.NewGeometry(geom).MarshalJSON()
			// if err != nil {
			// 	log.Println(err)
			// } else {
			// 	fmt.Println(string(j))
			// }
			downloadZoom(geom, minz)
			i++
		}
		bar.FinishPrint("The End!")
		minz++
	}
	wg.Wait() //wait for saving
}

func main() {
	cfgV = InitConf()
	minz := 10
	maxz := 12
	f := loadFeature("./geojson/beijing.geojson")
	start := time.Now()
	downloadDepth(f.Geometry, minz, maxz)
	secs := time.Since(start).Seconds()
	fmt.Printf("%.3fs finished...\n", secs)
}
