package service

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const defaultXFYunRTASRURL = "wss://rtasr.xfyun.cn/v1/ws"

// XFYunRTASRService 负责创建与讯飞 RTASR 官方 WebSocket 的连接。
type XFYunRTASRService struct {
	AppID  string
	APIKey string
	APIURL string
	Dialer *websocket.Dialer
}

// NewXFYunRTASRServiceFromEnv 从环境变量读取讯飞 RTASR 配置。
func NewXFYunRTASRServiceFromEnv() (*XFYunRTASRService, error) {
	appID := strings.TrimSpace(os.Getenv("XFYUN_RTASR_APP_ID"))
	apiKey := strings.TrimSpace(os.Getenv("XFYUN_RTASR_API_KEY"))
	apiURL := strings.TrimSpace(os.Getenv("XFYUN_RTASR_URL"))
	if apiURL == "" {
		apiURL = defaultXFYunRTASRURL
	}

	if appID == "" || apiKey == "" {
		return nil, fmt.Errorf("missing XFYUN_RTASR_APP_ID or XFYUN_RTASR_API_KEY")
	}

	return &XFYunRTASRService{
		AppID:  appID,
		APIKey: apiKey,
		APIURL: apiURL,
		Dialer: websocket.DefaultDialer,
	}, nil
}

// Dial 根据讯飞要求拼接鉴权参数并建立 WebSocket 连接。
func (s *XFYunRTASRService) Dial() (*websocket.Conn, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	signa := s.sign(ts)

	u, err := url.Parse(s.APIURL)
	if err != nil {
		return nil, fmt.Errorf("parse xfyun rtasr url failed: %w", err)
	}

	query := u.Query()
	query.Set("appid", s.AppID)
	query.Set("ts", ts)
	query.Set("signa", signa)
	u.RawQuery = query.Encode()

	conn, resp, err := s.Dialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial xfyun rtasr failed: %w (status=%s)", err, resp.Status)
		}
		return nil, fmt.Errorf("dial xfyun rtasr failed: %w", err)
	}

	return conn, nil
}

// sign 按讯飞文档生成 signa。
// 公式为：Base64(HmacSHA1(MD5(appid + ts), apiKey))。
func (s *XFYunRTASRService) sign(ts string) string {
	md5Hash := md5.Sum([]byte(s.AppID + ts))
	md5Text := fmt.Sprintf("%x", md5Hash)

	mac := hmac.New(sha1.New, []byte(s.APIKey))
	mac.Write([]byte(md5Text))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// xfyunRTASRResponse 对应讯飞 RTASR 返回的外层消息结构。
type xfyunRTASRResponse struct {
	Action  string `json:"action"`
	Code    string `json:"code"`
	Desc    string `json:"desc"`
	Message string `json:"message"`
	Data    string `json:"data"`
	SID     string `json:"sid"`
}

// xfyunSegmentEnvelope 对应讯飞返回的 data 字段结构，用于提取每个 seg_id 的转写文本。
type xfyunSegmentEnvelope struct {
	SegID int `json:"seg_id"`
	CN    struct {
		ST struct {
			RT []struct {
				WS []struct {
					CW []struct {
						W string `json:"w"`
					} `json:"cw"`
				} `json:"ws"`
			} `json:"rt"`
		} `json:"st"`
	} `json:"cn"`
}

// XFYunRTASRSession 表示一轮与讯飞实时转写的上游会话。
// 它负责持续读取上游结果、缓存分段文本，并在结束时拼出最终文本。
type XFYunRTASRSession struct {
	conn      *websocket.Conn
	segments  map[int]string
	segmentsM sync.Mutex

	readDone  chan struct{}
	readOnce  sync.Once
	closeOnce sync.Once
	resultMu  sync.Mutex
	lastError error
}

// NewXFYunRTASRSession 创建一个新的上游 RTASR 会话包装器。
func NewXFYunRTASRSession(conn *websocket.Conn) *XFYunRTASRSession {
	return &XFYunRTASRSession{
		conn:     conn,
		segments: make(map[int]string),
		readDone: make(chan struct{}),
	}
}

// StartReading 持续读取讯飞返回的流式结果。
// 这里不会主动回调前端，只是把分段文本缓存到本地，供结束时一次性取最终结果。
func (s *XFYunRTASRSession) StartReading() {
	go func() {
		defer s.readOnce.Do(func() { close(s.readDone) })
		for {
			_, message, err := s.conn.ReadMessage()
			if err != nil {
				s.setError(err)
				return
			}

			var resp xfyunRTASRResponse
			if err := json.Unmarshal(message, &resp); err != nil {
				s.setError(fmt.Errorf("decode xfyun rtasr response failed: %w", err))
				return
			}

			if resp.Code != "" && resp.Code != "0" {
				errMsg := resp.Desc
				if errMsg == "" {
					errMsg = resp.Message
				}
				if errMsg == "" {
					errMsg = "unknown xfyun rtasr error"
				}
				s.setError(errors.New(errMsg))
				return
			}

			// 讯飞会把增量结果放在 data 字段里，这里按 seg_id 累积。
			if resp.Action == "result" && resp.Data != "" {
				s.updateSegments(resp.Data)
			}
		}
	}()
}

// SendAudio 把 PCM 音频帧发给讯飞。
func (s *XFYunRTASRSession) SendAudio(data []byte) error {
	return s.conn.WriteMessage(websocket.BinaryMessage, data)
}

// SendEnd 按讯飞协议告诉上游“音频已经发送完毕”。
func (s *XFYunRTASRSession) SendEnd() error {
	return s.conn.WriteJSON(map[string]bool{"end": true})
}

// AwaitFinalText 等待本轮会话结束，并返回当前已聚合的最终文本。
// 如果上游以常见正常关闭结束，则仍返回已经拿到的文本结果。
func (s *XFYunRTASRSession) AwaitFinalText(timeout time.Duration) (string, error) {
	select {
	case <-s.readDone:
	case <-time.After(timeout):
		return s.currentText(), fmt.Errorf("wait xfyun final result timeout")
	}

	if err := s.error(); err != nil && !isExpectedRTASRDisconnect(err) {
		return s.currentText(), err
	}
	return s.currentText(), nil
}

// Close 主动关闭与讯飞的上游连接。
func (s *XFYunRTASRSession) Close() error {
	var err error
	s.closeOnce.Do(func() {
		err = s.conn.Close()
	})
	return err
}

// setError 记录本轮会话第一次出现的错误，避免后续错误覆盖根因。
func (s *XFYunRTASRSession) setError(err error) {
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	if s.lastError == nil {
		s.lastError = err
	}
}

// error 读取当前会话记录到的错误。
func (s *XFYunRTASRSession) error() error {
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	return s.lastError
}

// updateSegments 解析讯飞返回的 data，并按 seg_id 更新缓存文本。
func (s *XFYunRTASRSession) updateSegments(data string) {
	var payload xfyunSegmentEnvelope
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		s.setError(fmt.Errorf("decode xfyun segment failed: %w", err))
		return
	}

	var builder strings.Builder
	for _, rt := range payload.CN.ST.RT {
		for _, ws := range rt.WS {
			for _, cw := range ws.CW {
				builder.WriteString(cw.W)
			}
		}
	}

	// 讯飞的分段结果可能乱序到达，因此需要按 seg_id 存储，最后统一排序拼接。
	s.segmentsM.Lock()
	s.segments[payload.SegID] = builder.String()
	s.segmentsM.Unlock()
}

// currentText 把当前所有 seg_id 对应的文本按顺序拼接成一段完整文本。
func (s *XFYunRTASRSession) currentText() string {
	s.segmentsM.Lock()
	defer s.segmentsM.Unlock()

	keys := make([]int, 0, len(s.segments))
	for segID := range s.segments {
		keys = append(keys, segID)
	}
	sort.Ints(keys)

	var builder strings.Builder
	for _, segID := range keys {
		builder.WriteString(s.segments[segID])
	}
	return strings.TrimSpace(builder.String())
}

// isExpectedRTASRDisconnect 判断 RTASR 上游是否属于可接受的正常关闭场景。
func isExpectedRTASRDisconnect(err error) bool {
	if err == nil {
		return false
	}

	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "close 1000") ||
		strings.Contains(errMsg, "close 1005") ||
		strings.Contains(errMsg, "normal closure") ||
		strings.Contains(errMsg, "use of closed network connection")
}
