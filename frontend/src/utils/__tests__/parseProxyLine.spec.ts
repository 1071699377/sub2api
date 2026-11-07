import { describe, expect, it } from 'vitest'
import { parseProxyLine } from '../parseProxyLine'

describe('parseProxyLine', () => {
  it('parses scheme URL with auth', () => {
    expect(parseProxyLine('socks5://user:pass@proxy.example.com:1080')).toEqual({
      protocol: 'socks5',
      host: 'proxy.example.com',
      port: 1080,
      username: 'user',
      password: 'pass'
    })
  })

  it('parses scheme URL without auth', () => {
    expect(parseProxyLine('http://192.168.1.1:8080')).toEqual({
      protocol: 'http',
      host: '192.168.1.1',
      port: 8080,
      username: '',
      password: ''
    })
  })

  it('parses bracketed IPv6 URL', () => {
    expect(parseProxyLine('socks5://user:pass@[2001:db8::1]:1080')).toEqual({
      protocol: 'socks5',
      host: '2001:db8::1',
      port: 1080,
      username: 'user',
      password: 'pass'
    })
  })

  it('parses host:port:user:pass (vendor format)', () => {
    expect(parseProxyLine('45.127.248.174:5175:casbvjlp:ptekyk4vq965')).toEqual({
      protocol: 'http',
      host: '45.127.248.174',
      port: 5175,
      username: 'casbvjlp',
      password: 'ptekyk4vq965'
    })
  })

  it('keeps extra colons in password for host:port:user:pass', () => {
    expect(parseProxyLine('1.2.3.4:8080:user:p:a:ss')).toEqual({
      protocol: 'http',
      host: '1.2.3.4',
      port: 8080,
      username: 'user',
      password: 'p:a:ss'
    })
  })

  it('parses host:port', () => {
    expect(parseProxyLine('proxy.example.com:3128')).toEqual({
      protocol: 'http',
      host: 'proxy.example.com',
      port: 3128,
      username: '',
      password: ''
    })
  })

  it('parses user:pass@host:port without scheme', () => {
    expect(parseProxyLine('user:pass@192.168.1.1:1080')).toEqual({
      protocol: 'http',
      host: '192.168.1.1',
      port: 1080,
      username: 'user',
      password: 'pass'
    })
  })

  it('parses user:pass:host:port', () => {
    expect(parseProxyLine('casbvjlp:ptekyk4vq965:45.127.248.174:5175')).toEqual({
      protocol: 'http',
      host: '45.127.248.174',
      port: 5175,
      username: 'casbvjlp',
      password: 'ptekyk4vq965'
    })
  })

  it('rejects empty / whitespace', () => {
    expect(parseProxyLine('')).toBeNull()
    expect(parseProxyLine('   ')).toBeNull()
  })

  it('rejects invalid ports and unsupported schemes', () => {
    expect(parseProxyLine('http://example.com:port')).toBeNull()
    expect(parseProxyLine('ftp://example.com:21')).toBeNull()
    expect(parseProxyLine('1.2.3.4:99999:user:pass')).toBeNull()
    expect(parseProxyLine('1.2.3.4:8080:onlyuser')).toBeNull()
  })

  it('rejects bare IPv6 without brackets in URL form', () => {
    expect(parseProxyLine('socks5://2001:db8::1:1080')).toBeNull()
  })
})
