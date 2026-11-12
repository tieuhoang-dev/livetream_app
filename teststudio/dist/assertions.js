export function expectValue(value, negate = false) {
  const api = {};
  const assert = (ok, message) => {
    if (negate ? ok : !ok) throw new Error(message);
    return api;
  };
  const label = v => typeof v === 'string' ? JSON.stringify(v) : JSON.stringify(v) ?? String(v);
  for (const key of ['to', 'be', 'been', 'is', 'that', 'which', 'and', 'has', 'have', 'with', 'at', 'of', 'same']) {
    Object.defineProperty(api, key, {get: () => api});
  }
  Object.defineProperty(api, 'not', {get: () => expectValue(value, !negate)});
  Object.defineProperty(api, 'empty', {get: () => assert(value != null && (value.length === 0 || (typeof value === 'object' && Object.keys(value).length === 0)), 'Giá trị phải rỗng')});
  Object.defineProperty(api, 'true', {get: () => assert(value === true, 'Giá trị phải là true')});
  Object.defineProperty(api, 'false', {get: () => assert(value === false, 'Giá trị phải là false')});
  Object.defineProperty(api, 'exist', {get: () => assert(value != null, 'Giá trị phải tồn tại')});
  api.eql = expected => assert(JSON.stringify(value) === JSON.stringify(expected), `Mong đợi ${label(expected)}, nhận ${label(value)}`);
  api.equal = expected => assert(value === expected, `Mong đợi ${label(expected)}, nhận ${label(value)}`);
  api.equals = api.equal;
  api.a = api.an = type => assert(type === 'array' ? Array.isArray(value) : typeof value === type, `Mong đợi kiểu ${type}`);
  api.match = regex => assert(regex.test(value), 'Giá trị không khớp định dạng yêu cầu');
  api.lengthOf = n => assert(value?.length === n, `Mong đợi ${n} phần tử, nhận ${value?.length}`);
  api.property = key => assert(value != null && Object.prototype.hasOwnProperty.call(value, key), `Kiểm tra thuộc tính ${key}`);
  api.include = item => assert(typeof value?.includes === 'function' && value.includes(item), 'Giá trị không chứa nội dung mong đợi');
  api.above = n => assert(value > n, `Giá trị phải lớn hơn ${n}`);
  api.least = n => assert(value >= n, `Giá trị phải lớn hơn hoặc bằng ${n}`);
  return api;
}

export function redact(value) {
  if (Array.isArray(value)) return value.map(redact);
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) =>
    [key, /password|token|authorization|secret|^code$|reset_code|verification_code/i.test(key) ? '••••••' : redact(item)]));
  return value;
}
