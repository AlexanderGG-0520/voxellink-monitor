package main
import ("context"; "encoding/json"; "log"; "net/http"; "os"; "time"; "github.com/alexandergg-0520/voxellink-monitor/internal/minecraft")
func main(){ role:="api"; if len(os.Args)>1 { role=os.Args[1] }; switch role { case "api": api(); case "worker": worker(); case "bot": log.Print("Discord bot foundation ready; configure DISCORD_BOT_TOKEN to enable gateway adapter"); select{}; default: log.Fatalf("unknown role %q",role) } }
func api(){ mux:=http.NewServeMux(); mux.HandleFunc("/healthz",func(w http.ResponseWriter,r *http.Request){w.WriteHeader(http.StatusNoContent)}); mux.HandleFunc("/api/v1/probe",func(w http.ResponseWriter,r *http.Request){ host:=r.URL.Query().Get("host"); if host=="" { http.Error(w,"host is required",400); return }; result:=minecraft.PingJava(host,25565,5*time.Second); w.Header().Set("Content-Type","application/json"); json.NewEncoder(w).Encode(result) }); log.Fatal(http.ListenAndServe(env("HTTP_ADDR",":8080"),mux)) }
func worker(){ log.Print("monitor worker started; scheduler adapter awaits repository configuration"); <-context.Background().Done() }
func env(k,d string)string{if v:=os.Getenv(k);v!=""{return v};return d}
