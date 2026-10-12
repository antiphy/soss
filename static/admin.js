/* SOSS 管理后台前端脚本：原生 JS + fetch，能跑就行。 */
(function () {
  'use strict';

  var admin = window.SOSS_ADMIN || {};
  var page = admin.page;

  function q(sel) { return document.querySelector(sel); }
  function qa(sel) { return Array.prototype.slice.call(document.querySelectorAll(sel)); }

  /* 统一的 JSON 请求：出错时抛出带服务端 message 的 Error。 */
  function api(method, url, body) {
    var opts = { method: method, credentials: 'same-origin', headers: {} };
    if (body instanceof FormData) {
      opts.body = body;
    } else if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    return fetch(url, opts).then(function (res) {
      return res.text().then(function (text) {
        var data = null;
        try { data = text ? JSON.parse(text) : null; } catch (e) { data = null; }
        if (!res.ok) {
          var msg = (data && (data.message || data.Message)) || text || ('HTTP ' + res.status);
          throw new Error(msg);
        }
        return data;
      });
    });
  }

  function confirmBox(msg) { return window.confirm(msg); }

  function fail(err) {
    var msg = (err && err.message) ? err.message : String(err);
    window.alert('操作失败：' + msg);
  }

  function reload() { window.location.reload(); }

  /* ---------- 存储桶列表页 ---------- */
  function initBuckets() {
    var form = q('#create-form');
    if (form) {
      form.addEventListener('submit', function (e) {
        e.preventDefault();
        var name = form.elements.name.value.trim();
        var cls = parseInt(form.elements['class'].value, 10);
        if (!name) { return; }
        api('POST', '/admin/api/buckets', { name: name, class: cls })
          .then(reload).catch(fail);
      });
    }

    qa('[data-toggle-class]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var name = btn.getAttribute('data-toggle-class');
        var cls = parseInt(btn.getAttribute('data-class'), 10);
        api('PATCH', '/admin/api/buckets/' + encodeURIComponent(name), { class: cls })
          .then(reload).catch(fail);
      });
    });

    qa('[data-delete-bucket]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var name = btn.getAttribute('data-delete-bucket');
        if (!confirmBox('确定删除桶 "' + name + '" 吗？（只能删除空桶）')) { return; }
        api('DELETE', '/admin/api/buckets/' + encodeURIComponent(name))
          .then(reload).catch(fail);
      });
    });
  }

  /* ---------- 存储桶详情页 ---------- */
  function initBucket() {
    var bucket = admin.bucket;

    var upd = q('#update-class');
    if (upd) {
      upd.addEventListener('click', function () {
        var cls = parseInt(q('#class-select').value, 10);
        api('PATCH', '/admin/api/buckets/' + encodeURIComponent(bucket), { class: cls })
          .then(reload).catch(fail);
      });
    }

    var del = q('#delete-bucket');
    if (del) {
      del.addEventListener('click', function () {
        if (!confirmBox('确定删除桶 "' + bucket + '" 吗？（只能删除空桶）')) { return; }
        api('DELETE', '/admin/api/buckets/' + encodeURIComponent(bucket))
          .then(function () { window.location.href = '/admin/buckets'; }).catch(fail);
      });
    }

    var form = q('#upload-form');
    if (form) {
      form.addEventListener('submit', function (e) {
        e.preventDefault();
        var key = form.elements.key.value.trim();
        var file = form.elements.file.files[0];
        if (!key || !file) { return; }
        var fd = new FormData();
        fd.append('key', key);
        fd.append('file', file);
        api('POST', '/admin/api/buckets/' + encodeURIComponent(bucket) + '/objects/' + keyPath(key), fd)
          .then(function () { window.alert('上传成功'); reload(); }).catch(fail);
      });
    }

    var filter = q('#filter-form');
    if (filter) {
      filter.addEventListener('submit', function (e) {
        e.preventDefault();
        var prefix = filter.elements.prefix.value.trim();
        var url = '/admin/buckets/' + encodeURIComponent(bucket);
        if (prefix) { url += '?prefix=' + encodeURIComponent(prefix); }
        window.location.href = url;
      });
    }

    qa('[data-delete-object]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var key = btn.getAttribute('data-delete-object');
        if (!confirmBox('确定删除对象 "' + key + '" 的所有版本吗？')) { return; }
        api('DELETE', '/admin/api/buckets/' + encodeURIComponent(bucket) + '/objects/' + keyPath(key))
          .then(reload).catch(fail);
      });
    });

    qa('[data-versions]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var key = btn.getAttribute('data-versions');
        api('GET', '/admin/api/buckets/' + encodeURIComponent(bucket) + '/objects/' + keyPath(key) + '?versions=1')
          .then(showVersions).catch(fail);
      });
    });

    var close = q('#modal-close');
    if (close) {
      close.addEventListener('click', function () { q('#modal').classList.add('hidden'); });
    }
  }

  /* object key 可能含 "/"，逐个片段编码后拼接。 */
  function keyPath(key) {
    return String(key).split('/').map(encodeURIComponent).join('/');
  }

  function showVersions(data) {
    var list = (data && data.versions) || [];
    var bucket = admin.bucket;
    var html = '<table class="table"><thead><tr><th>版本</th><th>大小</th><th>ETag</th><th>创建时间</th><th></th></tr></thead><tbody>';
    if (!list.length) {
      html += '<tr><td colspan="5" class="empty">没有版本记录</td></tr>';
    }
    list.forEach(function (v) {
      var url = '/v1/buckets/' + encodeURIComponent(bucket) + '/objects/' + keyPath(data.object || '') + '?version=' + v.version;
      html += '<tr><td>' + v.version + '</td><td>' + (v.size || 0) + '</td><td>' + (v.etag || '') +
        '</td><td>' + new Date(v.created_at * 1000).toLocaleString() +
        '</td><td><a class="btn small" href="' + url + '">下载</a></td></tr>';
    });
    html += '</tbody></table>';
    q('#modal-title').textContent = '版本列表：' + (data.object || '');
    q('#modal-body').innerHTML = html;
    q('#modal').classList.remove('hidden');
  }

  if (page === 'buckets') { initBuckets(); }
  if (page === 'bucket') { initBucket(); }
})();
