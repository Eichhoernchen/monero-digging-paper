# Wasm scraper

This go program automates a chrome browser.

Its basic working principle is fetching input from stdin and producing json output on stdout.

The input is a line separated list of URLs which are passed to Chrome.

The output contains a json of the first 65kB of the HTML as well as the source to all WASM scripts and Websocket communication.


We used 10 parallel tabs for our paper.

