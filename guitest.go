// Copyright 2017 The Walk Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// schtasks /create /SC ONCE /ST 16:46
// /TR "C:\Users\xxxxxx\Documents\go\src\github.com\xxxxx\checknstart\checknstartx64.exe
// -msg youpi2 -type yala" /IT /TN folder\matache
//

package main

import (
	"flag"
	// "fmt"
	"log"
	"strings"
)

import (
	// "github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

func mymain() {
	var src *string
	var warntype *string
	src = flag.String("msg", "", "Message to display")
	warntype = flag.String("type", "", "Type of message")
	flag.Parse()
	// fmt.Printf("Will show [%s]", *src)
	if src != nil {
		win := MainWindow{
			Title:   "ROOM - Message d'information",
			Font:    Font{Family: "Verdana", PointSize: 12, Bold: false, Italic: false, Underline: false, StrikeOut: false},
			MinSize: Size{400, 300},
			Layout:  VBox{},
			Children: []Widget{
				Label{
					Font:    Font{Family: "Verdana", PointSize: 14, Bold: true, Italic: false, Underline: false, StrikeOut: false},
					MaxSize: Size{200, 0},
					Text:    *warntype,
				},
			},
		}

		arraymsg := strings.Split(*src, "||")
		// Display all elements.
		for i := range arraymsg {
			// fmt.Println(arraymsg[i])
			win.Children = append(win.Children, Label{
				MaxSize: Size{200, 0},
				Text:    arraymsg[i],
			},
			)
		}
		// Length is 3.
		// fmt.Println(len(arraymsg))
		if _, err := win.Run(); err != nil {
			log.Fatal(err)
		}
	}
}
