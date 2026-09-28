import unittest

from embedding_server import (
    append_eos,
    coerce_inputs,
    extract_vectors,
    fit_dimension,
    l2_normalize,
    openai_embeddings,
)


class EmbeddingMathTest(unittest.TestCase):
    def test_l2_normalize(self):
        vector = l2_normalize([3.0, 4.0])
        self.assertAlmostEqual(sum(value * value for value in vector), 1.0)

    def test_full_width_skips_truncation(self):
        fitted = fit_dimension([3.0, 4.0], dimension=2, native_dimension=2, mrl=False)
        self.assertEqual(len(fitted), 2)
        self.assertAlmostEqual(fitted[0], 0.6)

    def test_mrl_keeps_leading_coordinates(self):
        fitted = fit_dimension([3.0, 4.0, 0.0], dimension=2, native_dimension=3, mrl=True)
        self.assertEqual(len(fitted), 2)
        self.assertAlmostEqual(fitted[0], 0.6)
        self.assertAlmostEqual(fitted[1], 0.8)

    def test_truncate_without_mrl_is_rejected(self):
        with self.assertRaises(ValueError):
            fit_dimension([1.0, 0.0, 0.0], dimension=2, native_dimension=3, mrl=False)

    def test_append_eos_and_openai_shape(self):
        texts = append_eos(["北京"], "<|endoftext|>")
        self.assertEqual(texts, ["北京<|endoftext|>"])
        payload = openai_embeddings([[0.0, 1.0]], "Qwen/Qwen3-Embedding-4B")
        self.assertEqual(payload["data"][0]["index"], 0)
        self.assertEqual(payload["data"][0]["embedding"], [0.0, 1.0])

    def test_extract_orders_by_index(self):
        vectors = extract_vectors(
            {"data": [{"index": 1, "embedding": [2.0]}, {"index": 0, "embedding": [1.0]}]}
        )
        self.assertEqual(vectors, [[1.0], [2.0]])

    def test_coerce_inputs(self):
        self.assertEqual(coerce_inputs({"input": "hello"}), ["hello"])
        self.assertEqual(coerce_inputs({"input": ["a", "b"]}), ["a", "b"])
        with self.assertRaises(ValueError):
            coerce_inputs({"input": [1]})


if __name__ == "__main__":
    unittest.main()
