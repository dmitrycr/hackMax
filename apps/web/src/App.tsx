import { useEffect, useState } from 'react';
import { Button } from '@maxhub/max-ui';
import { getSystemStatus, type SystemStatus } from './shared/api/system';

const labels: Record<string, string> = {
  'go-api': 'Go API', postgres: 'PostgreSQL', 'document-processor': 'Python-сервис',
  'document-checking': 'Проверка документов', 'max-integration': 'Интеграция с MAX',
};
const states = { ready: 'Доступен', unavailable: 'Недоступен', not_implemented: 'Следующий этап' };

export function App() {
  const [system, setSystem] = useState<SystemStatus | null>(null);
  const [error, setError] = useState(false);
  const [revision, setRevision] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setError(false);
    setSystem(null);
    getSystemStatus(controller.signal).then(setSystem).catch(() => {
      if (!controller.signal.aborted) setError(true);
    });
    return () => controller.abort();
  }, [revision]);

  return (
    <main>
      <header><span className="brand">ДО ПОДАЧИ</span><span className="badge">Каркас проекта</span></header>
      <section className="intro">
        <p className="eyebrow">HackMax / рабочая среда</p>
        <h1>Основа для проверки<br />документов в MAX</h1>
        <p className="lead">Go управляет сервисом и правилами. Python извлекает данные и выполняет OCR. Здесь можно проверить, что компоненты подключены.</p>
      </section>
      <section className="card" aria-labelledby="status-title">
        <div className="section-heading"><h2 id="status-title">Состояние системы</h2><Button onClick={() => setRevision(v => v + 1)}>Обновить</Button></div>
        <div aria-live="polite">
          {error && <p role="alert">Не удалось подключиться к API. Проверьте запуск сервисов и повторите запрос.</p>}
          {!system && !error && <p>Проверяем подключение…</p>}
          {system && <ul className="status-list">{system.components.map(component => (
            <li key={component.name}><span>{labels[component.name] ?? component.name}</span><span className={`status ${component.status}`}>{states[component.status]}</span></li>
          ))}</ul>}
        </div>
      </section>
      <section className="areas" aria-label="Области проекта">
        <article className="card"><span className="number">01</span><h2>Кабинет заявителя</h2><p>Будущие загрузка файлов, замечания, повторная проверка и экспорт.</p></article>
        <article className="card"><span className="number">02</span><h2>Кабинет методиста</h2><p>Будущие бланки, правила, контрольные примеры и публикация требований.</p></article>
      </section>
      <footer>Инфраструктурный прототип. Загрузка документов, OCR и авторизация MAX пока не реализованы.</footer>
    </main>
  );
}

