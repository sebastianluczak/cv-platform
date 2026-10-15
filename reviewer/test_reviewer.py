import contextlib
import io
import json
import unittest
from unittest.mock import Mock, call, patch

from reviewer import reviewer


class OpenRouterRetryTests(unittest.TestCase):
    @patch.object(reviewer.time, "monotonic", side_effect=[10.0, 12.5])
    @patch.object(reviewer.requests, "post")
    def test_logs_request_start_status_and_duration(self, post, _monotonic):
        success = Mock(status_code=200)
        post.return_value = success

        with contextlib.redirect_stderr(io.StringIO()) as stderr:
            response = reviewer.post_with_retry("https://example.test")

        self.assertIs(response, success)
        self.assertIn("Sending OpenRouter request to https://example.test", stderr.getvalue())
        self.assertIn("responded with HTTP 200 after 2.50s", stderr.getvalue())

    @patch.object(reviewer.time, "sleep")
    @patch.object(reviewer.requests, "post")
    def test_retries_429_using_retry_after_header(self, post, sleep):
        rate_limited = Mock(status_code=429, headers={"Retry-After": "4"})
        success = Mock(status_code=200)
        post.side_effect = [rate_limited, success]

        with contextlib.redirect_stderr(io.StringIO()) as stderr:
            response = reviewer.post_with_retry("https://example.test")

        self.assertIs(response, success)
        self.assertEqual(post.call_count, 2)
        self.assertIn("Retry-After", stderr.getvalue())
        sleep.assert_called_once_with(4)

    @patch.object(reviewer.time, "sleep")
    @patch.object(reviewer.requests, "post")
    def test_raises_for_final_429_response(self, post, sleep):
        rate_limited = Mock(status_code=429, headers={})
        post.return_value = rate_limited

        with contextlib.redirect_stderr(io.StringIO()) as stderr:
            with self.assertRaisesRegex(
                RuntimeError, "still rate-limiting the request"
            ):
                reviewer.post_with_retry("https://example.test")

        self.assertEqual(post.call_count, reviewer.MAX_REQUEST_ATTEMPTS)
        self.assertEqual(stderr.getvalue().count("OpenRouter 429 response headers"), 3)
        self.assertEqual(
            sleep.call_args_list,
            [call(1), call(2)],
        )

    @patch.object(reviewer.time, "sleep")
    @patch.object(reviewer.requests, "post")
    def test_final_429_explains_remaining_quota_and_reset_time(self, post, sleep):
        rate_limited = Mock(
            status_code=429,
            headers={
                "X-RateLimit-Remaining": "0",
                "X-RateLimit-Reset": "1792108800000",
            },
        )
        post.return_value = rate_limited

        with contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(RuntimeError) as raised:
                reviewer.post_with_retry("https://example.test")

        self.assertIn("Requests remaining in the current window: 0", str(raised.exception))
        self.assertIn("2026-10-16T00:00:00+00:00", str(raised.exception))
        self.assertEqual(post.call_count, reviewer.MAX_REQUEST_ATTEMPTS)
        self.assertEqual(sleep.call_count, reviewer.MAX_REQUEST_ATTEMPTS - 1)

    @patch.object(reviewer.time, "time", return_value=1_700_000_000)
    @patch.object(reviewer.time, "sleep")
    @patch.object(reviewer.requests, "post")
    def test_retries_at_x_ratelimit_reset_timestamp(self, post, sleep, _now):
        rate_limited = Mock(
            status_code=429,
            headers={"X-RateLimit-Reset": "1700000005000"},
        )
        success = Mock(status_code=200)
        post.side_effect = [rate_limited, success]

        with contextlib.redirect_stderr(io.StringIO()) as stderr:
            response = reviewer.post_with_retry("https://example.test")

        self.assertIs(response, success)
        self.assertIn("X-RateLimit-Reset", stderr.getvalue())
        sleep.assert_called_once_with(5.0)

    @patch.object(reviewer.time, "sleep")
    @patch.object(reviewer.requests, "post")
    def test_prints_all_429_response_headers_to_stderr(self, post, sleep):
        rate_limited = Mock(
            status_code=429,
            headers={"Retry-After": "1", "X-RateLimit-Remaining": "0"},
        )
        success = Mock(status_code=200)
        post.side_effect = [rate_limited, success]

        with contextlib.redirect_stderr(io.StringIO()) as stderr:
            reviewer.post_with_retry("https://example.test")

        self.assertIn("Retry-After", stderr.getvalue())
        self.assertIn("X-RateLimit-Remaining", stderr.getvalue())
        sleep.assert_called_once_with(1)


class ReviewResponseValidationTests(unittest.TestCase):
    @patch.dict("os.environ", {"OPENROUTER_API_KEY": "test-key"})
    @patch.object(reviewer, "post_with_retry")
    def test_invalid_confidence_reports_response_and_safe_request_params(
        self, post_with_retry
    ):
        model_content = json.dumps(
            {
                "summary": "Review summary",
                "findings": [
                    {
                        "severity": "low",
                        "confidence": 1.2,
                        "file": "example.py",
                        "line": 10,
                        "title": "Example issue",
                        "explanation": "Explanation",
                        "suggested_fix": "Fix",
                    }
                ],
            }
        )
        raw_response = json.dumps(
            {
                "id": "response-id",
                "choices": [{"message": {"content": model_content}}],
                "usage": {"prompt_tokens": 123, "completion_tokens": 45},
            }
        )
        response = Mock(
            status_code=200,
            text=raw_response,
            **{"json.return_value": json.loads(raw_response)},
        )
        post_with_retry.return_value = response
        diff = "private source code"

        with self.assertRaisesRegex(
            ValueError, "Finding confidence must be between 0 and 1"
        ) as raised:
            reviewer.review(diff, "example/model")

        message = str(raised.exception)
        self.assertIn('"response_payload": {', message)
        self.assertIn('"raw_response_text":', message)
        self.assertIn('"usage": {', message)
        self.assertIn('"confidence": 1.2', message)
        self.assertIn('"model": "example/model"', message)
        self.assertIn('"temperature": 0.1', message)
        self.assertIn('"prompt_tokens": 123', message)
        self.assertIn('"characters": 19', message)
        self.assertNotIn(diff, message)

        request_params = post_with_retry.call_args.kwargs["json"]
        self.assertEqual(request_params["model"], "example/model")
        self.assertEqual(request_params["temperature"], 0.1)
        self.assertTrue(
            request_params["messages"][1]["content"].endswith(f"{diff}\n```")
        )


if __name__ == "__main__":
    unittest.main()
