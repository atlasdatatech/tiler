package main

import "math"

//TileSize 默认瓦片大小
const TileSize = 256

//ZoomMin 最小级别
const ZoomMin = 0

//ZoomMax 最大级别
const ZoomMax = 20

//Tile 自定义瓦片存储
type Tile struct {
	X, Y, Z uint
	C       []byte
}

func (tile Tile) flippedY() uint {
	zpower := math.Pow(2.0, float64(tile.Z))
	return uint(zpower) - 1 - tile.Y
}

//ZoomCount 级别&瓦片数
type ZoomCount struct {
	Level int
	Count int64
}

//Zoom 级别列表
type Zoom []ZoomCount

//TileFormat 切片格式
type TileFormat uint8

// Constants representing TileFormat types
const (
	UNKNOWN TileFormat = iota // UNKNOWN TileFormat cannot be determined from first few bytes of tile
	GZIP                      // encoding = gzip
	ZLIB                      // encoding = deflate
	PNG
	JPG
	PBF
	WEBP
)

// String returns a string representing the TileFormat
func (t TileFormat) String() string {
	switch t {
	case PNG:
		return "png"
	case JPG:
		return "jpg"
	case PBF:
		return "pbf"
	case WEBP:
		return "webp"
	default:
		return ""
	}
}

// ContentType returns the MIME content type of the tile
func (t TileFormat) ContentType() string {
	switch t {
	case PNG:
		return "image/png"
	case JPG:
		return "image/jpeg"
	case PBF:
		return "application/x-protobuf" // Content-Encoding header must be gzip
	case WEBP:
		return "image/webp"
	default:
		return ""
	}
}
