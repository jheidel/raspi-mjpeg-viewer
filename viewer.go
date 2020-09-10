package main

import (
	"bytes"
	"context"
	"sync"
	"strings"
	"mime"
	"image"
	"image/draw"
	"os"
	"fmt"
	"github.com/mattn/go-gtk/gdkpixbuf"
	"github.com/pixiv/go-libjpeg/jpeg"
	"encoding/json"
	"net/http"
	"flag"
	"github.com/mattn/go-gtk/glib"
	"github.com/mattn/go-gtk/gtk"
	"github.com/mattn/go-gtk/gdk"
	"mime/multipart"
	"time"
	 log "github.com/sirupsen/logrus"
)

var configPath = flag.String("config", "", "Path to the configuration file (required)")

type Config struct {
	Width int `json:"width"`
	Height int `json:"height"`
	MJPEGURL string `json:"mjpeg_url"`
}

func loadConfig() (*Config, error) {
	if *configPath == "" {
		return nil, fmt.Errorf("config file not specified")
	}
	f, err := os.Open(*configPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	c := &Config{}
	if err := dec.Decode(c); err != nil {
		return nil, err
	}
	return c, nil
}

type bufImage struct {
	pb *gdkpixbuf.Pixbuf
	rgba *image.RGBA
}

func newBufImage(bounds image.Rectangle) *bufImage {
	rgba := image.NewRGBA(bounds)

	var pbd gdkpixbuf.PixbufData
	pbd.Colorspace = gdkpixbuf.GDK_COLORSPACE_RGB
	pbd.HasAlpha = true
	pbd.BitsPerSample = 8
	pbd.Width = rgba.Bounds().Max.X
	pbd.Height = rgba.Bounds().Max.Y
	pbd.RowStride = rgba.Stride
	pbd.Data = rgba.Pix

	pb := gdkpixbuf.NewPixbufFromData(pbd)
	return &bufImage{
		pb: pb,
		rgba: rgba,
	}
}

var buf1, buf2 *bufImage

func toPixbuf(src image.Image) *gdkpixbuf.Pixbuf {
	b := src.Bounds()
	if buf1 == nil || buf2 == nil {
		buf1 = newBufImage(b)
		buf2 = newBufImage(b)
	}
	m := buf1
	buf1 = buf2
	buf2 = m

	draw.Draw(m.rgba, m.rgba.Bounds(), src, b.Min, draw.Src)
	return m.pb
}

var bufPool = sync.Pool{
	New: func() interface{} {
		// The Pool's New function should generally only return pointer
		// types, since a pointer can be put into the return interface
		// value without an allocation:
		return new(bytes.Buffer)
	},
}

func streamParts(ctx context.Context, wg *sync.WaitGroup, url string) <-chan *bytes.Buffer {
	ch := make(chan *bytes.Buffer)
	wg.Add(1)
	go func() {
		defer close(ch)
		defer wg.Done()
		first := true
		OUTER:
		for ctx.Err() == nil {
			if first {
				log.Infof("Connecting to %q", url)
			} else {
				log.Infof("Reconnecting to %q", url)
				time.Sleep(time.Second)
			}
			first = false

			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				log.Errorf("http request failed: %v", err)
				continue
			}

			tr := &http.Transport{
				IdleConnTimeout:    time.Minute,
				DisableCompression: true,
			}
			client := &http.Client{Transport: tr}

			res, err := client.Do(req)
			if err != nil {
				log.Errorf("client do failed: %v", err)
				continue
			}

			_, param, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
			if err != nil {
				log.Errorf("bad mime: %v", err)
				continue
			}

			reader := multipart.NewReader(res.Body, strings.Trim(param["boundary"], "-"))

			log.Infof("Connected!")
			for ctx.Err() == nil {
				part, err := reader.NextPart()
				if err != nil {
					log.Errorf("failed read: %v", err)
					continue OUTER
				}
				b := bufPool.Get().(*bytes.Buffer)
				b.Reset()
				if _, err := b.ReadFrom(part); err != nil {
					log.Errorf("failed read: %v", err)
					bufPool.Put(b)
					continue OUTER
				}

				select {
				case ch <- b:  // write downstream
				default:
					bufPool.Put(b) // discard
				}
			}
		}
	}()
	return ch
}

const BlankDuration = 15 * time.Second

func main() {
	flag.Parse()

	config, err := loadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	wg := &sync.WaitGroup{}
	ctx := context.Background()

	log.Infof("Starting")
	gtk.Init(&os.Args)
	gdk.ThreadsInit()

	window := gtk.NewWindow(gtk.WINDOW_TOPLEVEL)
	window.SetPosition(gtk.WIN_POS_CENTER)
	window.SetTitle("MJPEG Viewer")
	window.SetIconName("gtk-dialog-info")
	window.Connect("destroy", func(ctx *glib.CallbackContext) {
		gtk.MainQuit()
	}, "foo")
	window.ModifyBG(gtk.STATE_NORMAL, gdk.NewColorRGB(0, 0, 0))

	vbox := gtk.NewVBox(false, 1)

	connecting := gtk.NewLabel("Connecting...")
	connecting.ModifyFontEasy("DejaVu Serif 40")
	connecting.SetMarkup(`<span foreground="white">Connecting...</span>`)
	vbox.Add(connecting)

	imageBox := gtk.NewImage()
	imageBox.Hide()
	vbox.Add(imageBox)

	parts := streamParts(ctx, wg, config.MJPEGURL)

	blank := time.NewTicker(time.Second)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			select {
			case b := <-parts:
				blank.Reset(BlankDuration)
				img, err := jpeg.Decode(b, &jpeg.DecoderOptions{
					ScaleTarget: image.Rectangle{
						Min: image.Point{X: 0, Y: 0},
						Max: image.Point{X: config.Width, Y: config.Height},
					},
					DisableBlockSmoothing: true,
					DisableFancyUpsampling: true,
					DCTMethod: jpeg.DCTIFast,
				})
				bufPool.Put(b)
				if err != nil {
					log.Errorf("Failed to decode jpeg: %v", err)
					continue
				}

				pb := toPixbuf(img)

				gdk.ThreadsEnter()
				connecting.Hide()
				imageBox.Show()
				imageBox.SetFromPixbuf(pb)
				gdk.ThreadsLeave()

			case <-blank.C:
				gdk.ThreadsEnter()
				imageBox.Hide()
				connecting.Show()
				gdk.ThreadsLeave()

			case <-ctx.Done():
				return
			}
		}
	}()

	window.Add(vbox)
	window.SetSizeRequest(config.Width, config.Height)
	window.ShowAll()
	window.Fullscreen()
	gtk.Main()
	log.Warnf("Shutting down...")
	ctx.Done()
	wg.Wait()
	log.Infof("Exit")
}
