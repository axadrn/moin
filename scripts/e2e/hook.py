# A webhook for alerts: appends each body it gets to a file, one per line.
#   python3 hook.py <port> <file>
import http.server
import sys


class Hook(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        with open(sys.argv[2], "ab") as f:
            f.write(body + b"\n")
        self.send_response(204)
        self.end_headers()

    def log_message(self, *args):
        pass


http.server.HTTPServer(("127.0.0.1", int(sys.argv[1])), Hook).serve_forever()
