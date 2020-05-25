This directory contains the main test; just run `test.sh`. It's a full
functionality test; it verifies that the "outside port" works; and it also
frequently restarts the server (see `server.go`) in order to verify that no
HTTP request is dropped (the test fails/stops if a single HTTP request fails).
