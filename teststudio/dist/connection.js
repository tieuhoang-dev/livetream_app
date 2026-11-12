export function normalizeBackendUrl(value) {
  const url = new URL(String(value).trim());
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
    throw new Error('Nhập URL gốc HTTP/HTTPS của backend, ví dụ http://localhost:8080.');
  }
  if (/\/api(?:\/|$)/.test(url.pathname)) {
    throw new Error('Ô Địa chỉ backend chỉ cần URL gốc, ví dụ http://localhost:8080; không nhập /api/users/register.');
  }
  return url.href.replace(/\/+$/, '');
}

export function transportMessage(error, method, url) {
  if (['AbortError', 'TimeoutError'].includes(error.name)) return `${method} ${url}: đã dừng hoặc quá thời gian chờ.`;
  return `${method} ${url}: trình duyệt không cung cấp response HTTP (${error.name || 'Error'}: ${error.message || 'lỗi kết nối'}). Kiểm tra OPTIONS/CORS và quyền truy cập mạng nội bộ. Với localhost, mở bản Studio local. Lỗi này chưa xác nhận server đã xử lý request hay chưa.`;
}

// The login controller validates missing fields before calling the service.
// An empty login probes a JSON POST (including Authorization preflight) without
// creating an account, sending email, or attempting a real user's password.
export async function checkConnection(value, fetchImpl = fetch) {
  const base = normalizeBackendUrl(value);
  const probes = [
    {method: 'GET', path: '/api/products?page=1&page_size=1'},
    {method: 'POST', path: '/api/users/login', headers: {'Content-Type': 'application/json', Authorization: 'Bearer studio-connection-probe'}, body: '{}'}
  ];
  for (const probe of probes) {
    const url = base + probe.path;
    let response, body;
    try {
      response = await fetchImpl(url, {method: probe.method, headers: probe.headers, body: probe.body,
        signal: AbortSignal.timeout(10000), credentials: 'omit', redirect: 'error'});
      body = await response.json();
    } catch (error) {
      const prefix = probe.method === 'POST' ? 'GET đã được, nhưng POST JSON chưa qua. ' : '';
      throw new Error(prefix + transportMessage(error, probe.method, url));
    }
    const expected = probe.method === 'GET'
      ? response.ok && Array.isArray(body.data)
      : response.status === 400 && typeof body.error === 'string' && /[Ee]mail/.test(body.error) && /[Pp]assword/.test(body.error);
    if (!expected) throw new Error(`${probe.method} ${url}: HTTP ${response.status}, response khác API Livestream mong đợi.`);
  }
  return {base_url: base, get: true, jsonPost: true};
}
