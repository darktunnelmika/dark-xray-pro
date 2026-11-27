<?php
/**
 * Dark Xray Pro external API client: the partner API (dp_ keys, selling
 * reseller panels) and the reseller API (dr_ keys, a reseller's own bot or
 * site). One file, PHP 7.4+ with the curl extension, no other dependency.
 *
 *   require 'DarkXrayPro.php';
 *   $partner = new DarkXrayPro\Partner('https://panel.example.com:2087', 'dp_xxxx_yyyy');
 *   $panel = $partner->createPanel(['name' => 'Shop A', 'package' => 'gold', 'external_ref' => 'order-77'], 'order-77');
 *   echo $panel['message'];
 *
 *   $reseller = new DarkXrayPro\Reseller('https://panel.example.com:2087', 'dr_xxxx_yyyy');
 *   $r = $reseller->createClient(['name' => 'ali', 'gb' => 30, 'days' => 30], 'order-1001');
 *   echo $r['client']['sub_url'];
 *
 * Every change is sent with an Idempotency-Key (yours, or a random one), and
 * a request that fails on the network, with 429 or with 5xx is tried again
 * with the same key, so a retry never makes something twice. Errors throw
 * DarkXrayPro\ApiError with the HTTP status, the error code and the request id.
 */

namespace DarkXrayPro;

class ApiError extends \RuntimeException
{
    /** @var int HTTP status (0 when the server was not reached) */
    public $status;
    /** @var string stable error code, e.g. insufficient_credit */
    public $errorCode;
    /** @var string the server's X-Request-ID */
    public $requestId;
    /** @var mixed the decoded answer */
    public $body;

    public function __construct(int $status, string $errorCode, string $detail, string $requestId = '', $body = null)
    {
        parent::__construct($detail !== '' ? $detail : $errorCode, $status);
        $this->status = $status;
        $this->errorCode = $errorCode;
        $this->requestId = $requestId;
        $this->body = $body;
    }
}

class Client
{
    protected $base;
    protected $key;
    public $timeout = 30;
    public $retries = 3;
    /** @var array headers of the last answer, lower-case names */
    public $lastHeaders = [];

    public function __construct(string $baseUrl, string $key, int $timeout = 30)
    {
        $this->base = rtrim($baseUrl, '/');
        $this->key = $key;
        $this->timeout = $timeout;
    }

    protected static function idempotencyKey(): string
    {
        return 'sdk-' . bin2hex(random_bytes(12));
    }

    protected static function seg(string $v): string
    {
        return rawurlencode($v);
    }

    /**
     * Sends a request and returns the decoded JSON (or the raw body for a
     * non-JSON answer such as a QR code).
     */
    public function request(string $method, string $path, $body = null, array $query = [], ?string $idempotencyKey = null)
    {
        $url = $this->base . $path;
        $query = array_filter($query, function ($v) { return $v !== null && $v !== ''; });
        if ($query) {
            $url .= '?' . http_build_query($query);
        }
        $headers = ['Authorization: Bearer ' . $this->key, 'Accept: application/json'];
        if ($method !== 'GET') {
            $headers[] = 'Idempotency-Key: ' . ($idempotencyKey ?: self::idempotencyKey());
        }
        $payload = null;
        if ($body !== null) {
            $payload = json_encode($body, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
            $headers[] = 'Content-Type: application/json';
        }
        $attempt = 0;
        while (true) {
            $attempt++;
            $respHeaders = [];
            $ch = curl_init($url);
            curl_setopt_array($ch, [
                CURLOPT_CUSTOMREQUEST => $method,
                CURLOPT_RETURNTRANSFER => true,
                CURLOPT_HTTPHEADER => $headers,
                CURLOPT_TIMEOUT => $this->timeout,
                CURLOPT_CONNECTTIMEOUT => 10,
                CURLOPT_HEADERFUNCTION => function ($ch, $line) use (&$respHeaders) {
                    $parts = explode(':', $line, 2);
                    if (count($parts) === 2) {
                        $respHeaders[strtolower(trim($parts[0]))] = trim($parts[1]);
                    }
                    return strlen($line);
                },
            ]);
            if ($payload !== null) {
                curl_setopt($ch, CURLOPT_POSTFIELDS, $payload);
            }
            $raw = curl_exec($ch);
            $status = (int) curl_getinfo($ch, CURLINFO_HTTP_CODE);
            $netErr = $raw === false ? curl_error($ch) : '';
            curl_close($ch);
            $this->lastHeaders = $respHeaders;
            $retry = $raw === false || $status === 429 || $status >= 500;
            if ($retry && $attempt <= $this->retries) {
                $wait = isset($respHeaders['retry-after']) ? (int) $respHeaders['retry-after'] : $attempt * 2;
                sleep(max(1, min($wait, 30)));
                continue;
            }
            if ($raw === false) {
                throw new ApiError(0, 'network_error', $netErr);
            }
            $type = $respHeaders['content-type'] ?? '';
            $data = strpos($type, 'application/json') === 0 ? json_decode($raw, true) : $raw;
            if ($status >= 400) {
                $d = is_array($data) ? $data : [];
                throw new ApiError($status, $d['code'] ?? 'http_' . $status, $d['detail'] ?? (string) $raw, $d['request_id'] ?? ($respHeaders['x-request-id'] ?? ''), $data);
            }
            return $data;
        }
    }

    // ---- events and webhook (both APIs) ----

    public function events(int $after = 0, array $types = [], int $limit = 100)
    {
        return $this->request('GET', $this->prefix() . '/events', null, ['after' => $after, 'types' => implode(',', $types), 'limit' => $limit]);
    }

    public function webhook()
    {
        return $this->request('GET', $this->prefix() . '/webhook');
    }

    /** The answer has the signing secret when the webhook is new or $rotateSecret is true. */
    public function setWebhook(string $url, ?array $events = null, bool $rotateSecret = false, ?bool $enabled = null)
    {
        $b = ['url' => $url, 'rotate_secret' => $rotateSecret];
        if ($events !== null) {
            $b['events'] = $events;
        }
        if ($enabled !== null) {
            $b['enabled'] = $enabled;
        }
        return $this->request('PUT', $this->prefix() . '/webhook', $b);
    }

    public function deleteWebhook()
    {
        return $this->request('DELETE', $this->prefix() . '/webhook');
    }

    public function testWebhook()
    {
        return $this->request('POST', $this->prefix() . '/webhook/test');
    }

    public function webhookDeliveries(int $limit = 50)
    {
        return $this->request('GET', $this->prefix() . '/webhook/deliveries', null, ['limit' => $limit]);
    }

    protected function prefix(): string
    {
        return '';
    }
}

/** The partner API: make and run reseller panels with a dp_ key. */
class Partner extends Client
{
    protected function prefix(): string
    {
        return '/api/v2/partner';
    }

    private function rep(string $id, string $tail = ''): string
    {
        return '/api/v2/partner/reps/' . self::seg($id) . $tail;
    }

    public function me() { return $this->request('GET', '/api/v2/partner'); }
    public function packages() { return $this->request('GET', '/api/v2/partner/packages'); }
    public function stats(int $days = 30) { return $this->request('GET', '/api/v2/partner/stats', null, ['days' => $days]); }
    public function ledger(int $before = 0, int $limit = 100) { return $this->request('GET', '/api/v2/partner/ledger', null, ['before' => $before ?: null, 'limit' => $limit]); }

    /** $data: name, id, username, password, credit_gb, units, package, policy, telegram_ids, external_ref, note, low_credit_gb */
    public function createPanel(array $data, ?string $idempotencyKey = null) { return $this->request('POST', '/api/v2/partner/reps', $data, [], $idempotencyKey); }

    /** $query: q, enabled, external_ref, limit, offset. The total is in $this->lastHeaders['x-total-count']. */
    public function panels(array $query = []) { return $this->request('GET', '/api/v2/partner/reps', null, $query); }
    public function panel(string $id) { return $this->request('GET', $this->rep($id)); }
    public function findByExternalRef(string $ref)
    {
        $list = $this->panels(['external_ref' => $ref]);
        return $list[0] ?? null;
    }
    public function updatePanel(string $id, array $data) { return $this->request('PATCH', $this->rep($id), $data); }
    public function deletePanel(string $id, bool $withClients = false) { return $this->request('DELETE', $this->rep($id), null, ['clients' => $withClients ? '1' : null]); }
    public function addCredit(string $id, float $gb, int $units = 0, string $note = '', ?string $idempotencyKey = null) { return $this->request('POST', $this->rep($id, '/credit'), ['credit_gb' => $gb, 'units' => $units, 'note' => $note], [], $idempotencyKey); }
    public function revokeCredit(string $id, float $gb, int $units = 0, string $note = '', ?string $idempotencyKey = null) { return $this->request('POST', $this->rep($id, '/credit/revoke'), ['credit_gb' => $gb, 'units' => $units, 'note' => $note], [], $idempotencyKey); }
    public function panelLedger(string $id, int $before = 0, int $limit = 100) { return $this->request('GET', $this->rep($id, '/ledger'), null, ['before' => $before ?: null, 'limit' => $limit]); }
    public function overview(string $id) { return $this->request('GET', $this->rep($id, '/overview')); }
    public function panelClients(string $id, array $query = []) { return $this->request('GET', $this->rep($id, '/clients'), null, $query); }
    public function enable(string $id) { return $this->request('POST', $this->rep($id, '/enable')); }
    public function disable(string $id) { return $this->request('POST', $this->rep($id, '/disable')); }
    public function resetPassword(string $id, ?string $password = null) { return $this->request('POST', $this->rep($id, '/password'), $password === null ? null : ['password' => $password]); }
    public function revokeSessions(string $id) { return $this->request('POST', $this->rep($id, '/sessions/revoke')); }
    public function loginLink(string $id) { return $this->request('POST', $this->rep($id, '/login-link')); }
    public function apiKeys(string $id) { return $this->request('GET', $this->rep($id, '/apikeys')); }
    /** $data: name, scopes, allowed_ips, days, rate_per_min. The key is in the answer once. */
    public function createApiKey(string $id, array $data) { return $this->request('POST', $this->rep($id, '/apikeys'), $data); }
    public function deleteApiKey(string $id, string $keyId) { return $this->request('DELETE', $this->rep($id, '/apikeys/' . self::seg($keyId))); }
}

/** The reseller API: a reseller's own bot or site, with a dr_ key. */
class Reseller extends Client
{
    protected function prefix(): string
    {
        return '/api/v2/reseller';
    }

    private function c(string $name, string $tail = ''): string
    {
        return '/api/v2/reseller/clients/' . self::seg($name) . $tail;
    }

    public function me() { return $this->request('GET', '/api/v2/reseller/me'); }
    public function inbounds() { return $this->request('GET', '/api/v2/reseller/inbounds'); }
    public function plans() { return $this->request('GET', '/api/v2/reseller/plans'); }
    /** $data: action (create|renew), name (renew), plan, gb, days, start_on_first_use, max_ips, inbounds, product */
    public function quote(array $data) { return $this->request('POST', '/api/v2/reseller/quote', $data); }
    public function overview() { return $this->request('GET', '/api/v2/reseller/overview'); }
    public function ledger(int $before = 0, int $limit = 100) { return $this->request('GET', '/api/v2/reseller/ledger', null, ['before' => $before ?: null, 'limit' => $limit]); }

    /** $query: status, q, limit, offset */
    public function clients(array $query = []) { return $this->request('GET', '/api/v2/reseller/clients', null, $query); }
    /** $data: name, plan (volume|unlimited), gb, days, start_on_first_use, max_ips, device_limit, inbounds, note, telegram_id, product */
    public function createClient(array $data, ?string $idempotencyKey = null) { return $this->request('POST', '/api/v2/reseller/clients', $data, [], $idempotencyKey); }
    /** $data: prefix, count, start and the createClient fields */
    public function bulkCreate(array $data, ?string $idempotencyKey = null) { return $this->request('POST', '/api/v2/reseller/clients/bulk', $data, [], $idempotencyKey); }
    public function client(string $name) { return $this->request('GET', $this->c($name)); }
    /** $data: enabled, max_ips, inbounds, note, device_limit, telegram_id */
    public function updateClient(string $name, array $data) { return $this->request('PATCH', $this->c($name), $data); }
    public function deleteClient(string $name) { return $this->request('DELETE', $this->c($name)); }
    /** $data: plan, gb, days or product */
    public function renew(string $name, array $data, ?string $idempotencyKey = null) { return $this->request('POST', $this->c($name, '/renew'), $data, [], $idempotencyKey); }
    public function resetUsage(string $name) { return $this->request('POST', $this->c($name, '/reset')); }
    public function revokeLink(string $name) { return $this->request('POST', $this->c($name, '/revoke')); }
    public function enable(string $name) { return $this->request('POST', $this->c($name, '/enable')); }
    public function disable(string $name) { return $this->request('POST', $this->c($name, '/disable')); }
    public function links(string $name) { return $this->request('GET', $this->c($name, '/links')); }
    /** PNG bytes */
    public function qrPng(string $name, int $size = 320) { return $this->request('GET', $this->c($name, '/qr.png'), null, ['size' => $size]); }
    public function usage(string $name, int $days = 30) { return $this->request('GET', $this->c($name, '/usage'), null, ['days' => $days]); }
    public function devices(string $name) { return $this->request('GET', $this->c($name, '/devices')); }
    public function deleteDevice(string $name, string $hwid) { return $this->request('DELETE', $this->c($name, '/devices/' . self::seg($hwid))); }
    public function ips(string $name) { return $this->request('GET', $this->c($name, '/ips')); }
    public function online() { return $this->request('GET', '/api/v2/reseller/online'); }

    public function sellers() { return $this->request('GET', '/api/v2/reseller/sellers'); }
    public function createSeller(array $data) { return $this->request('POST', '/api/v2/reseller/sellers', $data); }
    public function updateSeller(string $id, array $data) { return $this->request('PATCH', '/api/v2/reseller/sellers/' . self::seg($id), $data); }
    public function sellerCredit(string $id, float $gb, int $units = 0, string $note = '') { return $this->request('POST', '/api/v2/reseller/sellers/' . self::seg($id) . '/credit', ['credit_gb' => $gb, 'units' => $units, 'note' => $note]); }
    public function sellerRevoke(string $id, float $gb, int $units = 0, string $note = '') { return $this->request('POST', '/api/v2/reseller/sellers/' . self::seg($id) . '/credit/revoke', ['credit_gb' => $gb, 'units' => $units, 'note' => $note]); }
}

/** Checking webhook deliveries. */
final class Webhook
{
    /**
     * True when the signature matches and the timestamp is recent.
     * $signature is X-Dark-Signature ("sha256=<hex>"), $timestamp X-Dark-Timestamp.
     */
    public static function verify(string $secret, string $timestamp, string $body, string $signature, int $tolerance = 300): bool
    {
        if ($timestamp === '' || !ctype_digit($timestamp) || abs(time() - (int) $timestamp) > $tolerance) {
            return false;
        }
        $want = 'sha256=' . hash_hmac('sha256', $timestamp . '.' . $body, $secret);
        return hash_equals($want, $signature);
    }

    /**
     * Reads the current request, checks it and returns its events (oldest
     * first). Answer 200 quickly after handling them; keep the last event id
     * to skip one that comes twice.
     */
    public static function events(string $secret): array
    {
        $body = file_get_contents('php://input');
        $ts = $_SERVER['HTTP_X_DARK_TIMESTAMP'] ?? '';
        $sig = $_SERVER['HTTP_X_DARK_SIGNATURE'] ?? '';
        if (!self::verify($secret, $ts, $body, $sig)) {
            throw new ApiError(401, 'bad_signature', 'the webhook signature does not match');
        }
        $data = json_decode($body, true);
        return $data['events'] ?? [];
    }
}
