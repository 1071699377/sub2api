import type { ProxyProtocol } from '@/types'

export interface ParsedProxyLine {
  protocol: ProxyProtocol
  host: string
  port: number
  username: string
  password: string
}

const DEFAULT_PROTOCOL: ProxyProtocol = 'http'

const URL_REGEX =
  /^(https?|socks5h?):\/\/(?:([^:@\[\]]+):([^@\[\]]+)@)?(\[[0-9a-f:.]+\]|[^:\[\]]+):(\d+)$/i

const AUTH_AT_HOST_REGEX =
  /^([^:@\[\]]+):([^@\[\]]+)@(\[[0-9a-f:.]+\]|[^:\[\]]+):(\d+)$/i

function parsePort(raw: string): number | null {
  if (!/^\d+$/.test(raw)) return null
  const port = Number.parseInt(raw, 10)
  if (port < 1 || port > 65535) return null
  return port
}

function normalizeHost(rawHost: string): string {
  return rawHost.replace(/^\[|\]$/g, '').trim()
}

function isValidHost(host: string): boolean {
  return host.length > 0 && !/\s/.test(host)
}

function fromParts(
  protocol: ProxyProtocol,
  host: string,
  port: number,
  username = '',
  password = ''
): ParsedProxyLine | null {
  const normalizedHost = normalizeHost(host)
  if (!isValidHost(normalizedHost)) return null
  return {
    protocol,
    host: normalizedHost,
    port,
    username: username.trim(),
    password: password.trim()
  }
}

/**
 * Parse one proxy line from quick-add input.
 * Supports:
 * - protocol://[user:pass@]host:port (incl. bracketed IPv6)
 * - user:pass@host:port
 * - host:port
 * - host:port:user:pass (password may contain extra ':')
 * - user:pass:host:port
 * Scheme-less lines default to http.
 */
export function parseProxyLine(line: string): ParsedProxyLine | null {
  const trimmed = line.trim()
  if (!trimmed) return null

  const urlMatch = trimmed.match(URL_REGEX)
  if (urlMatch) {
    const [, protocol, username, password, rawHost, portRaw] = urlMatch
    const port = parsePort(portRaw)
    if (port == null) return null
    return fromParts(
      protocol.toLowerCase() as ProxyProtocol,
      rawHost,
      port,
      username || '',
      password || ''
    )
  }

  const authAtMatch = trimmed.match(AUTH_AT_HOST_REGEX)
  if (authAtMatch) {
    const [, username, password, rawHost, portRaw] = authAtMatch
    const port = parsePort(portRaw)
    if (port == null) return null
    return fromParts(DEFAULT_PROTOCOL, rawHost, port, username, password)
  }

  const parts = trimmed.split(':')
  if (parts.length < 2) return null

  // host:port or host:port:user:pass...
  const portAsSecond = parsePort(parts[1])
  if (portAsSecond != null) {
    const host = parts[0]
    if (parts.length === 2) {
      return fromParts(DEFAULT_PROTOCOL, host, portAsSecond)
    }
    if (parts.length >= 4) {
      const username = parts[2]
      const password = parts.slice(3).join(':')
      return fromParts(DEFAULT_PROTOCOL, host, portAsSecond, username, password)
    }
    // host:port:user without password — reject as ambiguous/incomplete
    return null
  }

  // user:pass:host:port
  if (parts.length === 4) {
    const port = parsePort(parts[3])
    if (port == null) return null
    return fromParts(DEFAULT_PROTOCOL, parts[2], port, parts[0], parts[1])
  }

  return null
}
