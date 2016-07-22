package main

import (
        "fmt"
        "net/http"
        "log"
        "goji.io"
        "goji.io/pat"
        "golang.org/x/net/context"
)

type key int
const reqID key = 0

func hello(ctx context.Context, w http.ResponseWriter, r *http.Request) {
        name := pat.Param(ctx, "name")
        fmt.Fprintf(w, "Hello, %s!", name)
}

func ctxTest(ctx context.Context, w http.ResponseWriter, r *http.Request) {
        //name := pat.Param(ctx, "name")
        c := ctx.Value(reqID).(string)
        fmt.Fprintf(w, "Hello, %v!", c)
}

func NewRequestId() func(goji.Handler) goji.Handler {
    return func(h goji.Handler) goji.Handler {
        fn := func(ctx context.Context, w http.ResponseWriter, r *http.Request) {
            ctx = context.WithValue(ctx, reqID, "0123")
            h.ServeHTTPC(ctx, w, r)
        }
        return goji.HandlerFunc(fn)
    }
}

func main() {
        root := goji.NewMux()
        root.UseC(NewRequestId())
        root.HandleFuncC(pat.Get("/hello/:name"), hello)
        root.HandleFuncC(pat.Get("/*"), ctxTest)

        http.ListenAndServe("localhost:8000", root)
        log.Println("Listening on port 8000")
}
