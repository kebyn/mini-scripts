#!/usr/bin/env python3

__version__ = "0.7"
__all__ = ["SimpleHTTPRequestHandler"]

import argparse
import http.server
from html import escape
from io import BytesIO
from mimetypes import add_type, guess_type, init, inited
from os import error, fstat, listdir
from pathlib import Path
from re import findall
from shutil import copyfileobj
from ssl import create_default_context
from urllib.parse import quote, unquote, urlparse

# pylint: disable=missing-function-docstring
# pylint: disable=missing-class-docstring
# pylint: disable=invalid-name


class SimpleHTTPRequestHandler(http.server.BaseHTTPRequestHandler):

    """Simple HTTP request handler with GET/HEAD/POST commands.

    This serves files from the current directory and any of its
    subdirectories.  The MIME type for files is determined by
    calling the .guess_type() method. And can reveive file uploaded
    by client.

    The GET/HEAD/POST requests are identical except that the HEAD
    request omits the actual contents of the file.

    """

    server_version = "SimpleHTTPWithUpload/" + __version__
    if not inited:
        init()
    add_type("text/plain", ".log")

    def do_GET(self):
        """Serve a GET request."""
        f = self.send_head()
        if f:
            self.copyfile(f, self.wfile)
            f.close()

    def do_HEAD(self):
        """Serve a HEAD request."""
        f = self.send_head()
        if f:
            f.close()

    def do_POST(self):
        """Serve a POST request."""
        r, info = self.deal_post_data()
        print((r, info, "by: ", self.client_address))
        f = BytesIO()
        f.write(
            b"""
        <!DOCTYPE html>
        <html lang="en">
        <meta charset="UTF-8">
        <meta http-equiv="X-UA-Compatible" content="IE=edge">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>Upload Result Page</title>
        <body>
          <h2>Upload Result Page</h2>
        <hr>
        """
        )
        if r:
            f.write(b"<strong>Success:</strong>")
        else:
            f.write(b"<strong>Failed:</strong>")
        referer = self.headers.get("referer", "/")
        f.write(
            f"""
        {info}
        <br>
        <a href="{referer}">
            back home
        </a>
        </body>
        </html>
        """.encode("utf-8")
        )
        length = f.tell()
        f.seek(0)
        self.send_response(200)
        self.send_header("Content-type", "text/html")
        self.send_header("Content-Length", str(length))
        self.end_headers()
        if f:
            self.copyfile(f, self.wfile)
            f.close()

    def deal_post_data(self):
        content_type = self.headers["content-type"]
        if not content_type or "=" not in content_type:
            return (False, "Content-Type header doesn't contain boundary")
        boundary = content_type.split("=")[1].encode("utf-8")
        remainbytes = int(self.headers["content-length"])
        line = self.rfile.readline()
        remainbytes -= len(line)
        if boundary not in line:
            return (False, "Content NOT begin with boundary")
        line = self.rfile.readline()
        remainbytes -= len(line)
        fn = findall(
            r'Content-Disposition.*name="file"; filename="(.*)"', line.decode()
        )
        if not fn or not fn[0]:
            return (False, "Can't find out file name...")
        path = self.translate_path(self.path)
        fn = Path(path).joinpath(Path(fn[0]).name)
        line = self.rfile.readline()
        remainbytes -= len(line)
        line = self.rfile.readline()
        remainbytes -= len(line)
        try:
            out = open(fn, "wb")
        except IOError:
            return (
                False,
                "Can't create file to write, do you have permission to write?",
            )

        preline = self.rfile.readline()
        remainbytes -= len(preline)
        while remainbytes > 0:
            line = self.rfile.readline()
            remainbytes -= len(line)
            if boundary in line:
                preline = preline[0:-1]
                if preline.endswith(b"\r"):
                    preline = preline[0:-1]
                out.write(preline)
                out.close()
                return (True, f"File '{fn}' upload success!")
            else:
                out.write(preline)
                preline = line
        out.close()
        return (False, "Unexpect Ends of data.")

    def send_head(self):
        """Common code for GET and HEAD commands.

        This sends the response code and MIME headers.

        Return value is either a file object (which has to be copied
        to the outputfile by the caller unless the command was HEAD,
        and must be closed by the caller under all circumstances), or
        None, in which case the caller has nothing further to do.

        """
        path = self.translate_path(self.path)
        f = None
        if Path(path).is_dir():
            if not self.path.endswith("/"):
                # redirect browser - doing basically what apache does
                self.send_response(301)
                self.send_header("Location", self.path + "/")
                self.end_headers()
                return None
            for index in "index.html", "index.htm":
                index = Path(path).joinpath(index)
                if Path(index).exists():
                    path = index
                    break
            else:
                return self.list_directory(path)
        ctype = guess_type(path)[0] or "text/plain"
        try:
            # Always read in binary mode. Opening files in text mode may cause
            # newline translations, making the actual size of the content
            # transmitted *less* than the content-length!
            f = open(path, "rb")
        except IOError:
            self.send_error(404, "File not found")
            return None
        self.send_response(200)
        self.send_header("Content-type", f"{ctype};charset=utf-8")
        fs = fstat(f.fileno())
        self.send_header("Content-Length", str(fs[6]))
        self.send_header("Last-Modified", self.date_time_string(fs.st_mtime))
        self.end_headers()
        return f

    def list_directory(self, path):
        """Helper to produce a directory listing (absent index.html).

        Return value is either a file object, or None (indicating an
        error).  In either case, the headers are sent, making the
        interface the same as for send_head().

        """
        try:
            directorys = listdir(path)
        except error:
            self.send_error(404, "No permission to list directory")
            return None
        directorys.sort(key=lambda a: a.lower())
        f = BytesIO()
        displaypath = escape(unquote(self.path))
        f.write(
            f"""
        <!DOCTYPE html>
        <html lang="en">
        <meta charset="UTF-8">
        <meta http-equiv="X-UA-Compatible" content="IE=edge">
        <meta name="viewport" content="width=device-width, initial-scale=1.0">
        <title>Directory listing for {displaypath}</title>
        <body>
        <h2>Directory listing for {displaypath}</h2>
        <hr>
        <form ENCTYPE="multipart/form-data" method="post">
        <input name="file" type="file"/>
        <input type="submit" value="upload"/></form>
        <hr>
        <ul>
        """.encode("utf-8")
        )
        for name in directorys:
            fullname = Path(path).joinpath(name)
            displayname = linkname = name
            # Append / for directories or @ for symbolic links
            if Path(fullname).is_dir():
                displayname = name + "/"
                linkname = name + "/"
            if Path(fullname).is_symlink():
                displayname = name + "@"
                # Note: a link to a directory displays with @ and links with /
            f.write(
                f"""
                <li><a href="{quote(linkname)}">{escape(displayname)}</a>
                """.encode("utf-8")
            )
        f.write(
            """
                </ul>
                <hr>
                </body>
                </html>
                """.encode("utf-8")
        )
        length = f.tell()
        f.seek(0)
        self.send_response(200)
        self.send_header("Content-type", "text/html")
        self.send_header("Content-Length", str(length))
        self.end_headers()
        return f

    def translate_path(self, path):
        """Translate a /-separated PATH to the local filename syntax.

        Components that mean special things to the local file system
        (e.g. drive or directory names) are ignored.  (XXX They should
        probably be diagnosed.)

        """
        return Path().cwd().joinpath(Path(urlparse(path).path.strip("/")))

    def copyfile(self, source, outputfile):
        """Copy all data between two file objects.

        The SOURCE argument is a file object open for reading
        (or anything with a read() method) and the DESTINATION
        argument is a file object open for writing (or
        anything with a write() method).

        The only reason for overriding this would be to change
        the block size or perhaps to replace newlines by CRLF
        -- note however that this the default server uses this
        to copy binary data as well.

        """
        copyfileobj(source, outputfile)


if __name__ == "__main__":
    p = argparse.ArgumentParser(description="A Better Python3 HTTP Server")
    p.add_argument(
        "-p",
        "--port",
        type=int,
        default=8000,
        dest="port",
        action="store",
        help="the port for the http service to listen on",
    )
    p.add_argument(
        "-l",
        "--listen",
        type=str,
        default="0.0.0.0",
        dest="listen",
        action="store",
        help="the interface to bind to",
    )
    p.add_argument(
        "-s",
        "--ssl",
        action="store_true",
        default=False,
        dest="ssl",
        help="do serve https / ssl",
    )
    args = p.parse_args()

    try:
        httpd = http.server.HTTPServer(
            (args.listen, args.port), SimpleHTTPRequestHandler
        )
        if not args.ssl:
            print(
                "Serving HTTP on {l} port {p} (http://{l}:{p}/) ...".format(
                    l=args.listen, p=args.port
                )
            )
        else:
            context = create_default_context()
            context.load_cert_chain(certfile="crt.pem", keyfile="key.pem")
            httpd.socket = context.wrap_socket(httpd.socket, server_side=True)
            print(
                "Serving HTTPS on {l} port {p} (https://{l}:{p}/) ...".format(
                    l=args.listen, p=args.port
                )
            )
        httpd.serve_forever()

    except KeyboardInterrupt:
        print("^punch!")
