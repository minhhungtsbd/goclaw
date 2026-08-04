import base64
import json
import os
import subprocess
import tempfile
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def prompt_from_messages(messages, workspace):
    parts = ["Follow the system instructions and answer the latest user request."]
    for message in messages:
        role = str(message.get("role", "user")).upper()
        content = message.get("content", "")
        if isinstance(content, str):
            parts.append(f"\n[{role}]\n{content}")
            continue
        for item in content if isinstance(content, list) else []:
            if item.get("type") == "text":
                parts.append(f"\n[{role}]\n{item.get('text', '')}")
            elif item.get("type") == "image_url":
                url = item.get("image_url", {}).get("url", "")
                if url.startswith("data:image/") and "," in url:
                    header, encoded = url.split(",", 1)
                    ext = header.split(";")[0].split("/")[-1]
                    path = os.path.join(workspace, f"input-{uuid.uuid4().hex}.{ext}")
                    with open(path, "wb") as image:
                        image.write(base64.b64decode(encoded))
                    parts.append(f"Attached image: {path}")
    return "\n".join(parts)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_GET(self):
        if self.path == "/health":
            self.send_json(200, {"status": "ok"})
        else:
            self.send_json(404, {"error": {"message": "not found"}})

    def do_POST(self):
        if self.path != "/v1/chat/completions":
            self.send_json(404, {"error": {"message": "not found"}})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            request = json.loads(self.rfile.read(length))
            workspace = tempfile.mkdtemp(prefix="request-", dir=os.environ["AGY_WORKSPACE"])
            prompt = prompt_from_messages(request.get("messages", []), workspace)
            command = [os.environ["AGY_PATH"], "--print", "--output-format", "json", "--print-timeout", "5m"]
            if request.get("model") and request["model"] != "default":
                command += ["--model", request["model"]]
            command.append(prompt)
            result = subprocess.run(command, cwd=workspace, capture_output=True, text=True, timeout=310, check=True)
            output = json.loads(result.stdout)
            if output.get("status") != "SUCCESS" or not output.get("response"):
                raise RuntimeError("AGY returned empty content")
            usage = output.get("usage", {})
            self.send_json(200, {"id": f"agy-{uuid.uuid4().hex}", "object": "chat.completion", "created": int(time.time()), "model": request.get("model", "default"), "choices": [{"index": 0, "message": {"role": "assistant", "content": output["response"]}, "finish_reason": "stop"}], "usage": {"prompt_tokens": usage.get("input_tokens", 0), "completion_tokens": usage.get("output_tokens", 0), "total_tokens": usage.get("total_tokens", 0)}})
        except Exception as error:
            self.send_json(502, {"error": {"message": str(error), "type": "antigravity_runtime_error"}})

    def send_json(self, status, value):
        body = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


ThreadingHTTPServer(("0.0.0.0", int(os.environ["PORT"])), Handler).serve_forever()
