// How to run:
//
// 		go run ./cmd/dnn-detection/main.go [videosource] [modelfile] [configfile] [show window] ([backend] [device])
// TODO:
//
//		Check if the image exists

package main

import (
	"container/list"
	"encoding/json"
	"fmt"
	"image"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"net/http"
	_ "net/http/pprof"

	"gocv.io/x/gocv"

	//"github.com/hybridgroup/mjpeg"
	//"github.com/golang-collections/collections/stack"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
)

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web %v", err)
	}
	return tok
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

//GetFileContentType get mimetype
func GetFileContentType(out *os.File) (string, error) {

	// Only the first 512 bytes are used to sniff the content type.
	buffer := make([]byte, 512)

	_, err := out.Read(buffer)
	if err != nil {
		return "", err
	}

	// Use the net/http package's handy DectectContentType function. Always returns a valid
	// content-type by returning "application/octet-stream" if no others seemed to match.
	contentType := http.DetectContentType(buffer)
	out.Seek(0, os.SEEK_SET)

	return contentType, nil
}
func pushFile(jsonFile, fileName string) {
	b, err := ioutil.ReadFile(jsonFile)
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, drive.DriveScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config)

	srv, err := drive.New(client)
	if err != nil {
		log.Fatalf("Unable to retrieve drive Client %v", err)
	}

	file, err := os.Open(fileName)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	defer file.Close()
	//var convertedMimeType string

	//convertedMimeType := "application/vnd.google.drive.ext-type.jpg" // mimeType of file you want to convert on Google Drive https://developers.google.com/drive/api/v3/mime-types
	baseMimeType, err := GetFileContentType(file) // mimeType of file you want to upload
	fmt.Println(baseMimeType)
	if err != nil {
		baseMimeType = "image/jpg"
		log.Fatalf("Error: %v", err)
	}
	f := &drive.File{
		Name: filepath.Base(fileName),
		//MimeType: convertedMimeType,
	}
	res, err := srv.Files.Create(f).Media(file, googleapi.ContentType(baseMimeType)).Do()
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	fmt.Printf("%s https://drive.google.com/open?id=%s %s\n", res.Name, res.Id, res.MimeType)

	permissiondata := &drive.Permission{
		Type: "anyone",
		Role: "reader",
		//Domain:             "ebay.com",
		AllowFileDiscovery: true,
	}
	/*pres*/ _, err = srv.Permissions.Create(res.Id, permissiondata).Do()
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	//fmt.Printf("%s, %s\n", pres.Type, pres.Role)

}

type frameInfo struct {
	mat       gocv.Mat
	frameTime time.Time
}

func (f *frameInfo) Close() error {
	return f.mat.Close()
}

type record struct {
	lock  sync.Mutex
	frame *list.List
}

func (r *record) Len() int {
	//r.lock.Lock()
	l := r.frame.Len()
	//r.lock.Unlock()
	return l
}
func (r *record) PopFront() interface{} {
	//r.lock.Lock()
	e := r.frame.Remove(r.frame.Front())
	//r.lock.Unlock()
	return e
}

func (r *record) Front() *list.Element {
	//r.lock.Lock()
	e := r.frame.Front()
	//r.lock.Unlock()
	return e
}
func (r *record) PushBack(v interface{}) *list.Element {
	//r.lock.Lock()
	e := r.frame.PushBack(v)
	//r.lock.Unlock()
	return e
}
func (r *record) PushFront(v interface{}) *list.Element {
	//r.lock.Lock()
	e := r.frame.PushFront(v)
	//r.lock.Unlock()
	return e
}
func checkListFrameTime(r *record, getFrame chan bool) {

	for {
		select {
		//有新的影像儲存時 檢查Buffer頭一張是否超過時間
		case <-getFrame:
			if frame, ok := r.Front().Value.(*frameInfo); ok {
				if time.Now().Sub(frame.frameTime) > 7*time.Second {
					r.PopFront()
					frame.mat.Close()
				}
			} else {
				Dbgln("No Frame")
			}

		}
	}
}
func checkListFrameTimeV2(r *record, c chan time.Time) {

	for {
		select {
		//有新的影像儲存時 檢查Buffer頭一張是否超過時間
		case t := <-c:
			for {
				if r.Front() != nil {
					if frame, ok := r.Front().Value.(*frameInfo); ok {
						if t.Sub(frame.frameTime) > time.Duration(20)*time.Second {
							r.PopFront()
							frame.mat.Close()
						} else {
							break
						}
					} else {
						Dbgln("No Frame")
					}
				}
			}
		}
	}
}
func frameDetecter(net *gocv.Net, scaleFactor float64, size image.Point, mean gocv.Scalar,
	swapRB bool, crop bool, needDetect chan *list.Element, alarmChan chan *list.Element, getFrame chan time.Time) {
	var lastUploadTime = time.Now()
	killer := lastUploadTime
	var i int
	for {
		select {
		case e := <-needDetect:

			frame := e.Value.(*frameInfo)
			// convert image Mat to 300x300 blob that the object detector can analyze
			blob := gocv.BlobFromImage(frame.mat, scaleFactor, image.Pt(300, 300), mean, swapRB, crop)

			// feed the blob into the detector
			net.SetInput(blob, "")

			// run a forward pass thru the network
			prob := net.Forward("")

			if performDetection(&frame.mat, prob) {
				if time.Now().Sub(lastUploadTime) >= 10*time.Second {
					alarmChan <- e
					lastUploadTime = time.Now()
					killer = lastUploadTime
					i = 0
				}
			} else {
				if time.Now().Sub(lastUploadTime) >= 10*time.Second {
					i++
					killer = killer.Add(time.Duration(10*i-8) * time.Second)
					getFrame <- killer

				}
			}
			prob.Close()
			blob.Close()

		}
	}
}

var fps = 30

func sendVideo(r *list.List, t time.Time, getFrame chan time.Time) {
	//defer wg.Done()
	fileName := fmt.Sprintf("%s.avi", t.Format("2006_01_02_15_04_05"))

	img := r.Front().Value.(*frameInfo).mat
	writer, err := gocv.VideoWriterFile(fileName, "DIVX", float64(fps), img.Cols(), img.Rows(), true)
	if err != nil {
		fmt.Println("error opening video writer device")
		return
	}
	defer os.Remove(fileName)
	defer pushFile("credentials.json", fileName)
	defer writer.Close()
	//defer writer.Close()
	prevTime := r.Front().Value.(*frameInfo).frameTime
	for r.Front() != nil {
		e := r.Remove(r.Front())
		if frame, ok := e.(*frameInfo); ok {
			if err := writer.Write(frame.mat); err != nil {
				fmt.Println(err)
			}
			time.Sleep(frame.frameTime.Sub(prevTime))
			prevTime = frame.frameTime
		}
	}
	getFrame <- t
}

func sendJpeg(img gocv.Mat, t time.Time) {

	fileName := fmt.Sprintf("%s.jpg", t.Format("2006_01_02_15_04_05"))

	if gocv.IMWrite(fileName, img) {
		defer os.Remove(fileName)
		pushFile("credentials.json", fileName)
	} else {
		fmt.Println("IMWrite Fail")
	}

}
func uploadMedia(r *record, alarmChan chan *list.Element, getFrame chan time.Time) {

	for {
		select {
		case e := <-alarmChan:

			frameBuf := list.New()
			alarmTime := e.Value.(*frameInfo).frameTime

			go sendJpeg(e.Value.(*frameInfo).mat, alarmTime)

			for eN := e; eN != nil; eN = eN.Prev() {
				if frame, ok := eN.Value.(*frameInfo); ok {
					if alarmTime.Sub(frame.frameTime) < 5*time.Second {
						//Dbgln(frame.frameTime.Format("2006_01_02_15_04_05"), alarmTime.Format("2006_01_02_15_04_05"))
						frameBuf.PushFront(frame)
					} else {
						break
					}
				}
			}

			for eN := e.Next(); eN != nil; eN = eN.Next() {
				if frame, ok := eN.Value.(*frameInfo); ok {
					if frame.frameTime.Sub(alarmTime) < 5*time.Second {
						//Dbgln(frame.frameTime.Format("2006_01_02_15_04_05"), alarmTime.Format("2006_01_02_15_04_05"))
						frameBuf.PushBack(frame)
						for eN.Next() == nil {
							time.Sleep(time.Duration(1000) * time.Millisecond)
						}
					} else {
						break
					}
				}

			}
			go sendVideo(frameBuf, alarmTime, getFrame)

		}
	}
}

//var stream *mjpeg.Stream
func isOpenWindow(val string) bool {
	if v, err := strconv.ParseInt(val, 10, 64); err == nil {
		if v > 0 {
			return true
		}
	}
	return false
}

//Dbgln Dbgln
func Dbgln(args ...interface{}) {
	programCounter, _, line, _ := runtime.Caller(1)
	fn := runtime.FuncForPC(programCounter)
	//prefix := fmt.Sprintf("[%s:%s %d] %s", file, fn.Name(), line, fmt_)
	prefix := fmt.Sprintf("[%s %d]", fn.Name(), line)

	fmt.Printf("%s", prefix)
	fmt.Println(args...)
}

//Dbg Dbg
func Dbg(s string, args ...interface{}) {
	programCounter, _, line, _ := runtime.Caller(1)
	fn := runtime.FuncForPC(programCounter)
	//prefix := fmt.Sprintf("[%s:%s %d] %s", file, fn.Name(), line, s)
	prefix := fmt.Sprintf("[%s %d] %s", fn.Name(), line, s)
	fmt.Printf(prefix, args...)
	fmt.Println()
}
func main() {
	if len(os.Args) < 4 {
		fmt.Println("How to run:\ndnn-detection [videosource] [modelfile] [configfile] [show window]([backend] [device])")
		return
	}

	// parse args
	deviceID := os.Args[1]
	model := os.Args[2]
	config := os.Args[3]
	OPENWINDOW := isOpenWindow(os.Args[4])
	backend := gocv.NetBackendDefault
	alarmChan := make(chan *list.Element, 90*fps)
	//getFrameChan := make(chan bool, 90*fps)
	getFrameV2 := make(chan time.Time, 90*fps)
	needDetectChan := make(chan *list.Element, 90*fps)

	if len(os.Args) > 5 {
		backend = gocv.ParseNetBackend(os.Args[4])
	}

	target := gocv.NetTargetCPU
	if len(os.Args) > 6 {
		target = gocv.ParseNetTarget(os.Args[5])
	}

	// open capture device
	webcam, err := gocv.OpenVideoCapture(deviceID)
	//webcam, err := gocv.VideoCaptureFile(deviceID)
	if err != nil {
		fmt.Printf("Error opening video capture device: %v\n", deviceID)
		return
	}
	var window *gocv.Window
	defer webcam.Close()
	if OPENWINDOW {
		window = gocv.NewWindow("DNN Detection")
		defer window.Close()
	}

	img := gocv.NewMat()
	defer img.Close()

	// open DNN object tracking model
	net := gocv.ReadNet(model, config)
	if net.Empty() {
		fmt.Printf("Error reading network model from : %v %v\n", model, config)
		return
	}
	defer net.Close()
	net.SetPreferableBackend(gocv.NetBackendType(backend))
	net.SetPreferableTarget(gocv.NetTargetType(target))

	var ratio float64
	var mean gocv.Scalar
	var swapRGB bool

	//wg := sync.WaitGroup{}

	var recordManager record

	recordManager.frame = list.New()

	if filepath.Ext(model) == ".caffemodel" {
		ratio = 1.0
		mean = gocv.NewScalar(104, 177, 123, 0)
		swapRGB = false
	} else {
		ratio = 1.0 / 127.5
		mean = gocv.NewScalar(127.5, 127.5, 127.5, 0)
		swapRGB = true
	}
	/*stream = mjpeg.NewStream()
	fmt.Printf("Start reading device: %v\n", deviceID)
	go func() {
		http.Handle("/", stream)
		log.Fatal(http.ListenAndServe("0.0.0.0:8080", nil))
	}()*/
	//go checkListFrameTime(&recordManager, getFrameChan)
	go checkListFrameTimeV2(&recordManager, getFrameV2)
	go frameDetecter(&net, ratio, image.Pt(300, 300), mean, swapRGB, false, needDetectChan, alarmChan, getFrameV2)
	go uploadMedia(&recordManager, alarmChan, getFrameV2)
	var cnt uint
	var t time.Time
	for {

		if ok := webcam.Read(&img); !ok {
			fmt.Printf("Device closed: %v\n", deviceID)
			return
		}
		t = time.Now()

		if img.Empty() {
			continue
		}

		e := recordManager.PushBack(&frameInfo{img.Clone(), t})
		//getFrameChan <- true
		//Dbg("0 %p\n", e)
		if cnt%10 == 0 {
			needDetectChan <- e
		}
		cnt++
		//buf, _ := gocv.IMEncode(".jpg", img)
		//stream.UpdateJPEG(buf)

		if OPENWINDOW {
			window.IMShow(img)
			if window.WaitKey(1) >= 0 {
				break
			}
		}
	}
}

// performDetection analyzes the results from the detector network,
// which produces an output blob with a shape 1x1xNx7
// where N is the number of detections, and each detection
// is a vector of float values
// [batchId, classId, confidence, left, top, right, bottom]
func performDetection(frame *gocv.Mat, results gocv.Mat) (get bool) {

	for i := 0; i < results.Total(); i += 7 {
		confidence := results.GetFloatAt(0, i+2)
		if confidence > 0.5 {
			//left := int(results.GetFloatAt(0, i+3) * float32(frame.Cols()))
			//top := int(results.GetFloatAt(0, i+4) * float32(frame.Rows()))
			//right := int(results.GetFloatAt(0, i+5) * float32(frame.Cols()))
			//bottom := int(results.GetFloatAt(0, i+6) * float32(frame.Rows()))
			//gocv.Rectangle(frame, image.Rect(left, top, right, bottom), color.RGBA{0, 255, 0, 0}, 2)
			get = true
			break
		}
	}
	return
}
