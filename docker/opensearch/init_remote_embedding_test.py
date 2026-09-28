import unittest

from init_remote_embedding import connector_body, health_url, trusted_regex_for, vector_length


class RemoteEmbeddingPlanTest(unittest.TestCase):
    def test_trusted_regex_matches_local_and_compose_hosts(self):
        local = trusted_regex_for("http://127.0.0.1:18080/v1/embeddings")
        self.assertEqual(local, r"^http://127\.0\.0\.1:18080/.*$")
        compose = trusted_regex_for("http://embedding-qwen3:8080/v1/embeddings")
        self.assertIn(r"embedding\-qwen3", compose)
        self.assertTrue(compose.startswith("^http://"))

    def test_health_url(self):
        self.assertEqual(health_url("http://127.0.0.1:18080/v1/embeddings"), "http://127.0.0.1:18080/health")

    def test_connector_uses_openai_embedding_processors(self):
        body = connector_body(
            "ragflow_qwen3_embedding_4b",
            "http://embedding-qwen3:8080/v1/embeddings",
            "Qwen/Qwen3-Embedding-4B",
            "local-embedding",
        )
        action = body["actions"][0]
        self.assertEqual(action["pre_process_function"], "connector.pre_process.openai.embedding")
        self.assertEqual(action["post_process_function"], "connector.post_process.openai.embedding")
        self.assertIn("${parameters.input}", action["request_body"])
        self.assertNotIn('"${parameters.input}"', action["request_body"])
        self.assertEqual(action["url"], "http://embedding-qwen3:8080/v1/embeddings")
        self.assertEqual(body["parameters"]["model"], "Qwen/Qwen3-Embedding-4B")

    def test_batched_predict_counts_each_output_tensor(self):
        payload = {
            "inference_results": [
                {
                    "output": [
                        {"name": "sentence_embedding", "data": [0.1, 0.2]},
                        {"name": "sentence_embedding", "data": [0.3]},
                    ]
                }
            ]
        }
        self.assertEqual(vector_length(payload), [2, 1])


if __name__ == "__main__":
    unittest.main()
