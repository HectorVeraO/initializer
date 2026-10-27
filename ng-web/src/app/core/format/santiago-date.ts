const SANTIAGO_TIME_ZONE = 'America/Santiago';

const formatter = new Intl.DateTimeFormat('es-CL', {
  timeZone: SANTIAGO_TIME_ZONE,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23'
});

/** Formats an RFC 3339 instant as `dd/MM/yyyy HH:mm:ss` in America/Santiago; unparseable input is returned as is. */
export function formatSantiago(value: string): string {
  const instant = new Date(value);

  if (Number.isNaN(instant.getTime())) {
    return value;
  }

  const parts: Record<string, string> = {};

  for (const part of formatter.formatToParts(instant)) {
    parts[part.type] = part.value;
  }

  return `${parts['day']}/${parts['month']}/${parts['year']} ${parts['hour']}:${parts['minute']}:${parts['second']}`;
}
