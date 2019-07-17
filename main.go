// What it does:
//
// This example uses a deep neural network to perform object detection.
// It can be used with either the Caffe face tracking or Tensorflow object detection models that are
// included with OpenCV 3.4
//
// To perform face tracking with the Caffe model:
//
// Download the model file from:
// https://github.com/opencv/opencv_3rdparty/raw/dnn_samples_face_detector_20170830/res10_300x300_ssd_iter_140000.caffemodel
//
// You will also need the prototxt config file:
// https://raw.githubusercontent.com/opencv/opencv/master/samples/dnn/face_detector/deploy.prototxt
//
// To perform object tracking with the Tensorflow model:
//
// Download and extract the model file named "frozen_inference_graph.pb" from:
// http://download.tensorflow.org/models/object_detection/ssd_mobilenet_v1_coco_2017_11_17.tar.gz
//
// You will also need the pbtxt config file:
// https://gist.githubusercontent.com/dkurt/45118a9c57c38677b65d6953ae62924a/raw/b0edd9e8c992c25fe1c804e77b06d20a89064871/ssd_mobilenet_v1_coco_2017_11_17.pbtxt
//
// How to run:
//
// 		go run ./cmd/dnn-detection/main.go [videosource] [modelfile] [configfile] ([backend] [device])
//
// +build example

package main

import (
	"container/list"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
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
	fmt.Printf("%s, https://drive.google.com/open?id=%s, %s\n", res.Name, res.Id, res.MimeType)

	permissiondata := &drive.Permission{
		Type: "anyone",
		Role: "reader",
		//Domain:             "ebay.com",
		AllowFileDiscovery: true,
	}
	pres, err := srv.Permissions.Create(res.Id, permissiondata).Do()
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	fmt.Printf("%s, %s\n", pres.Type, pres.Role)

}

type frameInfo struct {
	mat       gocv.Mat
	frameTime time.Time
}

type record struct {
	lock  sync.Mutex
	frame *list.List
}

func (r *record) Len() int {
	r.lock.Lock()
	l := r.frame.Len()
	r.lock.Unlock()
	return l
}
func (r *record) PopFront() interface{} {
	r.lock.Lock()
	e := r.frame.Remove(r.frame.Front())
	r.lock.Unlock()
	return e
}

func (r *record) Front() interface{} {
	r.lock.Lock()
	e := r.frame.Front()
	r.lock.Unlock()
	return e
}
func (r *record) PushBack(v interface{}) *list.Element {
	r.lock.Lock()
	e := r.frame.PushBack(v)
	r.lock.Unlock()
	return e
}
func (r *record) PushFront(v interface{}) *list.Element {
	r.lock.Lock()
	e := r.frame.PushFront(v)
	r.lock.Unlock()
	return e
}
func checkListFrameTime(r *record, getFrame chan bool) {

	for {
		select {
		//有新的影像儲存時 檢查Buffer頭一張是否超過時間
		case <-getFrame:
			if frame, ok := r.Front().(frameInfo); ok {
				if time.Now().Sub(frame.frameTime) > 10*time.Second {
					r.PopFront()
				}
			}

		}
	}
}
func frameDetecter(net *gocv.Net, scaleFactor float64, size image.Point, mean gocv.Scalar,
	swapRB bool, crop bool, needDetect chan *list.Element, alarmChan chan *list.Element) {

	for {
		select {
		case e := <-needDetect:
			frame := e.Value.(frameInfo)
			// convert image Mat to 300x300 blob that the object detector can analyze
			blob := gocv.BlobFromImage(frame.mat, scaleFactor, image.Pt(300, 300), mean, swapRB, false)

			// feed the blob into the detector
			net.SetInput(blob, "")

			// run a forward pass thru the network
			prob := net.Forward("")

			if performDetection(&frame.mat, prob) {
				alarmChan <- e
			}

			prob.Close()
			blob.Close()
		}
	}
}

var fps = 30

func sendVideo(r *list.List, t time.Time) {
	//defer wg.Done()
	fileName := fmt.Sprintf("%s.avi", t.Format("2006_01_02_15_04_05"))
	img := r.Front().Value.(frameInfo).mat
	writer, err := gocv.VideoWriterFile(fileName, "DIVX", float64(fps), img.Cols(), img.Rows(), true)
	if err != nil {
		fmt.Println("error opening video writer device")
		return
	}
	defer os.Remove(fileName)
	//defer writer.Close()

	for r.Len() > 0 {
		e := r.Remove(r.Front())
		if frame, ok := e.(frameInfo); ok {
			if err := writer.Write(frame.mat); err != nil {
				fmt.Println(err)
			}
			//time.Sleep(time.Duration(1000/fps) * time.Millisecond)
		} else {
			fmt.Println("Fail Mat")
		}
	}
	writer.Close()
	pushFile("credentials.json", fileName)
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
func uploadMedia(alarmChan chan *list.Element) {
	var lastUploadTime time.Time
	for {
		select {
		case e := <-alarmChan:
			if time.Now().Sub(lastUploadTime) >= 10*time.Second {
				frameBuf := list.New()
				alarmTime := e.Value.(frameInfo).frameTime

				go sendJpeg(e.Value.(frameInfo).mat, alarmTime)

				e_bk := e
				for ; e != nil; e = e.Prev() {
					frame := e.Value.(frameInfo)
					if alarmTime.Sub(frame.frameTime) < 5*time.Second {
						frameBuf.PushFront(frame)
					} else {
						break
					}
				}
				for e := e_bk.Next(); e != nil; e = e.Next() {
					frame := e.Value.(frameInfo)
					if frame.frameTime.Sub(alarmTime) < 5*time.Second {
						frameBuf.PushBack(frame)
					} else {
						break
					}
				}
				go sendVideo(frameBuf, alarmTime)
				lastUploadTime = time.Now()
			}
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
	alarmChan := make(chan *list.Element, 10*fps)
	getFrameChan := make(chan bool, 20*fps)
	needDetectChan := make(chan *list.Element, 30*fps)

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

	var recordManager [3]record
	for i, _ := range recordManager {
		recordManager[i].frame = list.New()
	}

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
	go checkListFrameTime(&recordManager[0], getFrameChan)
	go frameDetecter(&net, ratio, image.Pt(300, 300), mean, swapRGB, false, needDetectChan, alarmChan)
	go uploadMedia(alarmChan)
	for {
		if ok := webcam.Read(&img); !ok {
			fmt.Printf("Device closed: %v\n", deviceID)
			return
		}
		if img.Empty() {
			continue
		}

		e := recordManager[0].PushBack(frameInfo{img, time.Now()})
		getFrameChan <- true
		needDetectChan <- e
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
			left := int(results.GetFloatAt(0, i+3) * float32(frame.Cols()))
			top := int(results.GetFloatAt(0, i+4) * float32(frame.Rows()))
			right := int(results.GetFloatAt(0, i+5) * float32(frame.Cols()))
			bottom := int(results.GetFloatAt(0, i+6) * float32(frame.Rows()))
			gocv.Rectangle(frame, image.Rect(left, top, right, bottom), color.RGBA{0, 255, 0, 0}, 2)
			get = true
		}
	}
	return
}
