import base64
import importlib.util
import pathlib
import tempfile
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("server.py")
SPEC = importlib.util.spec_from_file_location("antigravity_runtime_server", MODULE_PATH)
SERVER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SERVER)


def image_part(label):
    return {
        "type": "image_url",
        "image_url": {
            "url": "data:image/png;base64," + base64.b64encode(label.encode()).decode(),
        },
    }


class PromptFromMessagesTest(unittest.TestCase):
    def test_only_latest_user_image_is_forwarded(self):
        messages = [
            {"role": "user", "content": [image_part("old-image")]},
            {"role": "assistant", "content": "Earlier response"},
            {"role": "user", "content": [{"type": "text", "text": "Analyze this"}, image_part("new-image")]},
        ]

        with tempfile.TemporaryDirectory() as workspace:
            prompt = SERVER.prompt_from_messages(messages, workspace)
            images = list(pathlib.Path(workspace).glob("input-*.png"))

            self.assertEqual(len(images), 1)
            self.assertEqual(images[0].read_bytes(), b"new-image")
            self.assertIn("[LATEST USER IMAGE 1]", prompt)
            self.assertIn("Analyze this", prompt)


if __name__ == "__main__":
    unittest.main()
