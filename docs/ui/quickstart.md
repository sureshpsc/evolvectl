# Local UI

`evolvectl ui serve --host 127.0.0.1 --port 0` serves saved runs. It does not rescan and it does not edit source. Binding any non-loopback address is refused.

`evolvectl report --run <id> --format html --output report.html` writes one offline file. It does not load remote scripts, fonts, or images.

Timestamps in the report are UTC.
