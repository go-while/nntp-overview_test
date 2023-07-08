package main

import (
    "fmt"
    "log"
    "math/rand"
    "os"
    "os/signal"
    "runtime"
    "time"
    "github.com/go-while/go-utils"
    "github.com/go-while/nntp-overview"
)

var (
    NUM_CPUS int = 2
    DEBUG bool = true
    OVERVIEW_WORKERS int = 8
    OVERVIEW_QUEUE int = OVERVIEW_WORKERS*2
    MAX_OPEN_MMAPS int = 500
    MAX_KNOWN_MESSAGEIDS = 100
    OV_OPENER int = OVERVIEW_WORKERS
    OV_CLOSER int = OVERVIEW_WORKERS*2
    OVERVIEW_DIR string = "test_overview"

    stop_server_chan = make(chan bool, 1)       // used to signal that server is stopping
    overview_input_channel chan overview.OVL    // gets assigned by Load_Overview() later
)


func main() {
    start_time := utils.Now()
    runtime.GOMAXPROCS(NUM_CPUS)
    rand.Seed(1) // predictable random
    debug_ov_handler := DEBUG // || DEBUG
    overview_input_channel = overview.Load_Overview(OVERVIEW_WORKERS, OVERVIEW_QUEUE, MAX_OPEN_MMAPS, MAX_KNOWN_MESSAGEIDS, OV_OPENER, OV_CLOSER, stop_server_chan, debug_ov_handler)
    if overview_input_channel == nil {
        log.Printf("ERROR overview.Load_Overview returned nil channel")
        os.Exit(1)
    }

    test_max := 100           // generate this many articles per run, higher max will only flood memory with headers
    parallel := 10              // runs 'N' GO_main_test in parallel
    test := parallel*test_max     // generate this many articles to test creating overviews per go routine

    main_done := make(chan bool, parallel)
    counter_chan := make(chan uint64, parallel)
    processed_chan := make(chan uint64, 1)
    counter_chan <- 0
    go GO_counter(counter_chan, processed_chan)
    for i:=1; i<=parallel; i++ {
        go GO_main_test(i, parallel, main_done, test, test_max, counter_chan)
    }

    // Setting up signal capturing SIGINT (kill -2)
    os_stop := make(chan os.Signal, 1)
    signal.Notify(os_stop, os.Interrupt)

    wait_for := 0
    //closed_main := false
    forever:
    for {
        select {
            case <- os_stop:
                break forever

            case retbool, ok := <- main_done:
                if retbool == true {
                    wait_for++
                    log.Printf("main: done %d/%d", wait_for, parallel)
                    if wait_for == parallel {
                        close(main_done)
                        //break forever
                    }
                } else
                if !ok { // main_done channel is closed
                    if wait_for == parallel {
                        close(overview_input_channel)
                        log.Printf("main: closed overview_input_channel")
                        break forever
                    } else {
                        log.Printf("ERROR wait_for=%d != parallel=%d", wait_for, parallel)
                    }
                }
        }
    }

    wait_closing:
    for {
        len_ovi := len(overview_input_channel)
        if len_ovi == 0 {
            log.Printf("main: close(overview_input_channel)")
            close_server("main()")
            break wait_closing
        }
        log.Printf("main: waiting overview_input_channel=%d", len_ovi)
        time.Sleep(500 * time.Millisecond)
    }
    overview.Watch_overview_Workers(OVERVIEW_WORKERS, overview_input_channel)
    close(counter_chan)
    processed := <- processed_chan
    log.Printf("QUIT Overview runtime=%d processed=%d", utils.Now()-start_time, processed)

} // end func main


func GO_main_test(id int, parallel int, main_done chan bool, test int, test_max int, counter_chan chan uint64) {
    // testing app integration
    defer done(id, parallel, main_done)
    init := 1972 // just any year

    articles_done, remain := 0, test

    start_timer := utils.UnixTimeMilliSec()
    forever:
    for {
        step_timer := utils.UnixTimeMilliSec()
        var articles []ARTICLE
        if remain <= 0 {
            break forever
        }

        if test > test_max { // dont flood the memory
            test = test_max
        }
        articles, init = fake_article(init, test)  // generate fake articles
        remain -= test
        log.Printf("GO_main_test %d id=%d: test %d articles +todo=%d", utils.Nano(), id, len(articles), remain)

        for i, article := range articles {  // loop the fake articles
            count_chan_inc(counter_chan) // count them

            ovl := overview.Extract_overview("?", article.head) // pass header to extract_overview and receive an ovl object

            if ovl.Checksum != overview.OVL_CHECKSUM {
                log.Printf("GO_main_test id=%d: ERROR ovl.Checksum=%d != overview.OVL_CHECKSUM=%d ovl='%v' i=%d", id, ovl.Checksum, overview.OVL_CHECKSUM, ovl, i)
                return
            }

            // add info about article to ovl object
            ovl.Bytes = article.headsize+article.bodysize
            ovl.Lines = article.bodylines
            ovl.ReaderCachedir = OVERVIEW_DIR // where to place overview files
            ovl.Retchan = make(chan []overview.ReturnChannelData, 1)

            if DEBUG {
                log.Printf("GO_main_test id=%d: overview_input_channel=%d/%d", id, len(overview_input_channel), cap(overview_input_channel))
            }
            // pass extracted ovl header values to overview worker
            overview_input_channel <- ovl
            // wait for response if overview has been created or not

            // the retdata contains multiples: for every newsgroup a msgnum
            retdata := <- ovl.Retchan
            for j, data := range retdata {
                log.Printf("GO_main_test id=%d: i=%d data[%d]: retbool=%t msgnum=%d newsgroup='%s' msgid='%s'", id, i, j, data.Retbool, data.Msgnum, data.Newsgroup, ovl.Messageid )
                if data.Retbool {
                    // activemap.upHI(data.Newsgroup) // up the activemap HI value for this group
                }
            }

            articles_done++
        } // end for range headers
        log.Printf("GO_main_test %d id=%d: tested %d articles remain=%d took=%d ms", utils.Nano(), id, len(articles), remain, utils.UnixTimeMilliSec() - step_timer)
        if remain <= 0 {
            break forever
        }
    } // end for forever

    took := utils.UnixTimeMilliSec() - start_timer
    log.Printf("GO_main_test id=%d: returned took=%d ms", id, took)
} // end func GO_main_test


type ARTICLE struct {
    head []string
    headsize int
    bodylines int
    bodysize int
}


func fake_article(init int, max int) ([]ARTICLE, int) {
    var articles []ARTICLE
    bef := "test"
    done := 0
    for i := init; i <= init+max*4; i+=4 {
        var article ARTICLE
        c := randomChars(4)
        from := "From: from="+c+"@"+c+" ("+c+")"
        subj := "Subject: subject="+c+c+c+c
        date := fmt.Sprintf("Date: 29 Feb %d 00:00:00 UTC", i)
        msgid := "Message-ID: <"+c+"@"+c+".com>"
        ref := "References: <"+bef+"@"+bef+".com>"
        bef = c
        ng :=  "Newsgroups: ab.test,ab.test1 , ab.test2 ,ab.test3,  ab.test4,ab.test5  ,  ab.test5  ,   ab.test4,ab.test5"
        //ng :=  "Newsgroups: ab.test"
        article.head = []string{ ref, date, from, subj, msgid, ng, "", } // order should not matter only the final empty string is important?
        for _, line := range article.head {
            article.headsize += len(line)
        }
        article.bodylines, article.bodysize = fake_body()
        articles = append(articles, article)
        init = i
        done++
        if done >= max { break }
    }
    return articles, init
} // end func fake_article


func fake_body() (lines int, size int) {
    // just return random ints for bodylines and assume bodysize: multiply lines with 100 chars per line
    //lines = NonZeroRandomInt(1,128)
    //size = lines*100
    lines = 100
    size = lines*100
    return
} // end func fake_body


// generates a random string of fixed size
func randomChars(size int) string {
    /*
    charset := "0123456789abcdef"
    buf := make([]byte, size)
    for i := 0; i < size; i++ {
        //
        buf[i] = charset[rand.Intn(len(charset))]
    }
    return string(buf)
    */
    return "abcd"
} // end func randomChars


func NonZeroRandomInt(a int, b int) int {
    //rand.Seed(time.Now().UnixNano())
    n := a + rand.Intn(b-a+1)
    // if DEBUG { log.Printf("NonZeroRandomInt: a=%d, b=%d, n=%d", a, b, n) }
    return n
} // end NonZeroRandomInt


func done(id int, parallel int, main_done chan bool) {
    log.Printf("return main_done id=%d", id)
    main_done <- true
} // end func done


func count_chan_inc(counter_chan chan uint64) {
    counter_chan <- 1
} // end func count_chan_inc


func count_chan_get(counter_chan chan uint64) uint64 {
    log.Printf("counter_chan_get counter_chan=%d", len(counter_chan))
    value := <- counter_chan
    counter_chan <- value
    log.Printf("counter_chan_get value=%d", value)
    return value
} // end func count_chan_get


func close_server(src string) {
    log.Printf("close_server(src='%s')", src)
    if !is_closed_server() {
        stop_server_chan <- true
        close(stop_server_chan)
    }
}


func is_closed_server() bool {
    isclosed := false

    // try reading a true from channel
    select {
        case has_closed, ok := <- stop_server_chan:
            // ok will be !ok if channel is closed!
            if !ok || has_closed {
                isclosed = true
            }
        default:
            // if nothing in, defaults breaks select
            isclosed = false
    }

    return isclosed
} // end func is_closed_server


func GO_counter(counter_chan chan uint64, processed_chan chan uint64) {
    processed := <- counter_chan
    log.Printf("Boot GO_Counter: processed=%d counter_chan=%d/%d", processed, len(counter_chan), cap(counter_chan))
    for_counter:
    for {
        select {
            case count, ok := <- counter_chan:
                if !ok {
                    break for_counter
                }
                if count > 0 {
                    processed += count
                }
        }
    }
    log.Printf("GO_counter: processed=%d", processed)
    processed_chan <- processed
} // end func GO_counter

