//go:build windows && production && browserlive

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The fixture records browser-generated events; it never calls Host.Execute,
// dispatches DOM input, fills controls, or tells the service that work succeeded.
type browserLivePage struct {
	server           *httptest.Server
	name, nonce      string
	mu               sync.Mutex
	events           []browserLivePageEvent
	invalid          int
	canvasX, canvasY int
	canvasEvents     []browserLiveCanvasPageClick
	canvasInvalid    map[string]int
}
type browserLivePageEvent struct {
	Kind       string          `json:"kind"`
	Name       string          `json:"name,omitempty"`
	City       string          `json:"city,omitempty"`
	Filter     string          `json:"filter,omitempty"`
	SKU        string          `json:"sku,omitempty"`
	Trusted    bool            `json:"trusted"`
	ScrollY    float64         `json:"scrollY,omitempty"`
	X          float64         `json:"x,omitempty"`
	Y          float64         `json:"y,omitempty"`
	ClientX    float64         `json:"clientX,omitempty"`
	ClientY    float64         `json:"clientY,omitempty"`
	CanvasRect browserLiveRect `json:"canvasRect,omitempty"`
	Viewport   browserLiveSize `json:"viewport,omitempty"`
	DPR        float64         `json:"dpr,omitempty"`
}

func newBrowserLivePage(name string) *browserLivePage {
	f := &browserLivePage{name: name, nonce: browserID()}
	// Vary the visual target. Clicking the canvas center is not an acceptance pass.
	f.canvasX = 45 + int(f.nonce[0])%5*155
	f.canvasY = 150 + int(f.nonce[1])%3*115
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}
func (f *browserLivePage) address() string { return f.server.URL + "/" + f.nonce + "/" }
func (f *browserLivePage) goal() string {
	switch f.name {
	case "click":
		return "点击页面中的‘确认测试’一次。成功条件：页面显示‘测试完成’，不要重复点击。"
	case "form":
		return "在中文表单中将姓名填写为‘测试访客’，城市下拉框选择‘上海’，然后点击‘提交登记’一次。成功条件：页面显示‘登记完成：测试访客，上海’。"
	case "scroll":
		return "向下滚动页面，找到商品筛选区，在关键字中输入‘青瓷’，点击‘应用筛选’，再点击筛选结果中‘选择青瓷杯’。成功条件：页面显示‘已选择：青瓷杯’。不要选择其他商品。"
	case "canvas":
		return "这是纯视觉 Canvas 场景。请观察截图，点击绿色的‘确认图形’矩形内部一次，不要点击红色区域。成功条件：画布显示‘图形已确认’。目标的位置只在图像中可见，不要猜测坐标。"
	default:
		return "unsupported fixture"
	}
}
func (f *browserLivePage) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; form-action 'none'; frame-ancestors 'none'")
	base := "/" + f.nonce + "/"
	if r.Method == http.MethodGet && r.URL.Path == base {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, f.html())
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != base+"event" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Origin") != f.server.URL {
		http.NotFound(w, r)
		return
	}
	var ev browserLivePageEvent
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	err := d.Decode(&ev)
	var trailing any
	if err != nil || d.Decode(&trailing) != io.EOF {
		http.Error(w, "invalid fixture event", 400)
		return
	}
	f.mu.Lock()
	valid := f.accepts(ev) && len(f.events) < 12
	f.recordCanvasPageClick(ev, valid)
	if valid {
		f.events = append(f.events, ev)
	} else {
		f.invalid++
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if !valid {
		w.WriteHeader(422)
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": valid})
}
func (f *browserLivePage) accepts(e browserLivePageEvent) bool {
	if !e.Trusted {
		return false
	}
	switch f.name {
	case "click":
		return e.Kind == "click" && len(f.events) == 0
	case "form":
		return e.Kind == "submit" && e.Name == "测试访客" && e.City == "sh" && len(f.events) == 0
	case "scroll":
		if e.ScrollY < 500 {
			return false
		}
		return (e.Kind == "filter" && e.Filter == "青瓷" && len(f.events) == 0) || (e.Kind == "choose" && e.SKU == "celadon-cup" && len(f.events) == 1 && f.events[0].Kind == "filter")
	case "canvas":
		return e.Kind == "canvas" && e.X >= float64(f.canvasX) && e.X <= float64(f.canvasX+100) && e.Y >= float64(f.canvasY) && e.Y <= float64(f.canvasY+66) && len(f.events) == 0
	}
	return false
}
func (f *browserLivePage) verify() (int, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := 1
	if f.name == "scroll" {
		want = 2
	}
	if f.invalid != 0 {
		return len(f.events), "invalid_or_duplicate_page_event"
	}
	if len(f.events) != want {
		return len(f.events), "server_result_not_verified"
	}
	return len(f.events), "passed"
}
func (f *browserLivePage) html() string {
	prefix := `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>ORCA synthetic browser task</title><style>body{margin:0;padding:24px;font:18px "Microsoft YaHei","Segoe UI",sans-serif;color:#222;background:#fff}main{max-width:780px}label{display:block;margin:20px 0}input,select,button{font:inherit;padding:10px;border:1px solid #aab2b9;border-radius:6px}button{background:#e6edf6;cursor:pointer}output{display:block;margin-top:24px;font-weight:600}</style></head><body>`
	common := `async function record(value){const r=await fetch('event',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(value)});return r.ok && (await r.json()).ok;}`
	var content, script string
	switch f.name {
	case "click":
		content = `<main><h1>合成点击测试</h1><button id="confirm">确认测试</button><output id="result"></output></main>`
		script = `document.getElementById('confirm').addEventListener('click',async e=>{if(await record({kind:'click',trusted:e.isTrusted})){document.getElementById('result').textContent='测试完成';document.getElementById('confirm').disabled=true;}});`
	case "form":
		content = `<main><h1>访客登记</h1><label>姓名<input id="name" aria-label="姓名" autocomplete="off"></label><label>城市<select id="city" aria-label="城市"><option value="">请选择城市</option><option value="bj">北京</option><option value="sh">上海</option><option value="sz">深圳</option></select></label><button id="submit">提交登记</button><output id="result"></output></main>`
		script = `document.getElementById('submit').addEventListener('click',async e=>{const name=document.getElementById('name').value,city=document.getElementById('city').value;if(await record({kind:'submit',name,city,trusted:e.isTrusted})){document.getElementById('result').textContent='登记完成：'+name+'，上海';document.getElementById('submit').disabled=true;}});`
	case "scroll":
		content = `<main><h1>商品目录</h1><p>筛选区位于页面底部。</p><div style="height:1300px;background:repeating-linear-gradient(#fff,#fff 79px,#edf0f3 80px)"></div><section><h2>商品筛选</h2><label>关键字<input id="filter" aria-label="关键字" autocomplete="off"></label><button id="apply">应用筛选</button><div id="matches"></div><output id="result"></output></section></main>`
		script = `document.getElementById('apply').addEventListener('click',async e=>{const filter=document.getElementById('filter').value;if(await record({kind:'filter',filter,scrollY:window.scrollY,trusted:e.isTrusted})){document.getElementById('matches').innerHTML='<p>找到商品：青瓷杯</p><button id="choose">选择青瓷杯</button>';document.getElementById('choose').addEventListener('click',async e=>{if(await record({kind:'choose',sku:'celadon-cup',scrollY:window.scrollY,trusted:e.isTrusted})){document.getElementById('result').textContent='已选择：青瓷杯';document.getElementById('choose').disabled=true;}});document.getElementById('apply').disabled=true;}});`
	case "canvas":
		content = `<style>body{padding:0;overflow:hidden}canvas{display:block;width:100vw;height:100vh}</style><canvas id="scene" width="900" height="600"></canvas>`
		script = fmt.Sprintf(`const c=document.getElementById('scene'),g=c.getContext('2d');g.fillStyle='#f6f8fa';g.fillRect(0,0,900,600);g.fillStyle='#222';g.font='24px "Microsoft YaHei",sans-serif';g.fillText('请点击绿色的确认图形',30,55);g.fillStyle='#df5757';g.fillRect(400,280,100,66);g.fillStyle='#fff';g.font='18px "Microsoft YaHei",sans-serif';g.fillText('请勿点击',412,318);g.fillStyle='#299865';g.fillRect(%d,%d,100,66);g.fillStyle='#fff';g.fillText('确认图形',%d,%d);let done=false;c.addEventListener('click',async e=>{if(done)return;const r=c.getBoundingClientRect(),x=(e.clientX-r.left)*900/r.width,y=(e.clientY-r.top)*600/r.height;if(await record({kind:'canvas',x,y,trusted:e.isTrusted,clientX:e.clientX,clientY:e.clientY,canvasRect:{x:r.x,y:r.y,width:r.width,height:r.height},viewport:{width:innerWidth,height:innerHeight},dpr:devicePixelRatio})){done=true;g.fillStyle='#f6f8fa';g.fillRect(0,0,900,600);g.fillStyle='#187249';g.font='30px "Microsoft YaHei",sans-serif';g.fillText('图形已确认',300,300);}});`, f.canvasX, f.canvasY, f.canvasX+12, f.canvasY+40)
	}
	return prefix + content + "<script>" + common + script + "</script></body></html>"
}

func TestBrowserLivePageGradingOffline(t *testing.T) {
	for _, name := range []string{"click", "form", "scroll", "canvas"} {
		t.Run(name, func(t *testing.T) {
			f := newBrowserLivePage(name)
			defer f.server.Close()
			if _, status := f.verify(); status == "passed" {
				t.Fatal("self-report or empty fixture passed")
			}
			if f.accepts(browserLivePageEvent{Kind: "click"}) {
				t.Fatal("untrusted event accepted")
			}
			var events []browserLivePageEvent
			switch name {
			case "click":
				events = []browserLivePageEvent{{Kind: "click", Trusted: true}}
			case "form":
				events = []browserLivePageEvent{{Kind: "submit", Name: "测试访客", City: "sh", Trusted: true}}
			case "scroll":
				events = []browserLivePageEvent{{Kind: "filter", Filter: "青瓷", ScrollY: 1000, Trusted: true}, {Kind: "choose", SKU: "celadon-cup", ScrollY: 1100, Trusted: true}}
			case "canvas":
				events = []browserLivePageEvent{{Kind: "canvas", X: float64(f.canvasX + 30), Y: float64(f.canvasY + 20), Trusted: true}}
			}
			for _, ev := range events {
				body, _ := json.Marshal(ev)
				r := httptest.NewRequest(http.MethodPost, f.address()+"event", strings.NewReader(string(body)))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", f.server.URL)
				w := httptest.NewRecorder()
				f.serve(w, r)
				if w.Code != 200 {
					t.Fatal("correct synthetic event rejected")
				}
			}
			if count, status := f.verify(); count != len(events) || status != "passed" {
				t.Fatal("independent result missing")
			}
			if f.accepts(events[len(events)-1]) {
				t.Fatal("duplicate submit accepted")
			}
		})
	}
}
