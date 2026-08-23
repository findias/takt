// Поле формы: когда форма молчит, когда говорит и куда уходит фокус.
//
// Проверяется не разметка, а обещание из плана этапа: форма проходится
// с клавиатуры от первого поля до отказа сервера и обратно, и на каждом
// шаге понятно, что произошло. Ни один шаг этой дороги не виден
// ни в типах, ни глазами на снимке: снимок показывает форму в покое,
// а всё, ради чего форма и написана, случается между нажатиями.

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { Field, FormError, useFormErrors } from './Field.tsx'

/** Форма из двух полей: короче настоящей, устроена так же. */
function Форма({ onSend = async (_email: string) => {} }: { onSend?: (email: string) => Promise<void> }) {
  const form = useFormErrors()
  return (
    <form
      ref={form.ref}
      noValidate
      onSubmit={async (e) => {
        e.preventDefault()
        const found = form.check(e.currentTarget)
        if (Object.keys(found).length > 0) {
          form.report(found)
          return
        }
        const email = new FormData(e.currentTarget).get('email')
        try {
          await onSend(String(email))
          form.clear()
        } catch (err) {
          form.report({ email: (err as Error).message })
        }
      }}
    >
      <Field label="Почта" hint="Рабочая, не личная" {...form.field('email')}>
        {(bind) => <input {...bind} name="email" type="email" required />}
      </Field>
      <Field label="Пароль" {...form.field('password')}>
        {(bind) => <input {...bind} name="password" type="password" required minLength={8} />}
      </Field>
      <FormError>{form.formError}</FormError>
      <button type="submit">Войти</button>
    </form>
  )
}

describe('поле формы', () => {
  it('подпись, подсказка и отказ читаются вместе с полем', async () => {
    const user = userEvent.setup()
    render(<Форма />)

    // Поле находится по подписи — тем же способом, каким его находит
    // человек и диктор. `placeholder` так найти нельзя.
    const почта = screen.getByLabelText('Почта')
    // Подсказка связана до всякого отказа: требование сказано заранее,
    // а не выясняется по отказу.
    expect(почта.getAttribute('aria-describedby')).toBeTruthy()
    expect(описание(почта)).toContain('Рабочая, не личная')

    await user.click(screen.getByRole('button', { name: 'Войти' }))

    // Отказ приехал к описанию поля, а не вместо подсказки: человек
    // должен видеть и правило, и то, чем ответ ему не подошёл.
    await waitFor(() => expect(почта.getAttribute('aria-invalid')).toBe('true'))
    expect(описание(почта)).toContain('Рабочая, не личная')
    expect(описание(почта)).toContain('Заполните')
  })

  it('до отправки форма молчит', async () => {
    const user = userEvent.setup()
    render(<Форма />)

    // Пустое поле в наполовину заполненной форме — не ошибка,
    // а середина работы. Ругаться на неё значит ругаться на каждый
    // первый символ.
    await user.type(screen.getByLabelText('Почта'), 'не-почта')
    await user.tab()
    await user.tab()

    expect(screen.getByLabelText('Почта').getAttribute('aria-invalid')).toBeNull()
    expect(document.body.textContent).not.toContain('опечатка')
  })

  it('на отправке отвергнуты все, а фокус — на первом сверху', async () => {
    const user = userEvent.setup()
    render(<Форма />)

    await user.click(screen.getByRole('button', { name: 'Войти' }))

    // Все разом, а не по одному: браузерные пузырьки показывают первый
    // отказ и молчат об остальных, и форма из пяти полей чинится
    // впятеро дольше, чем нужно.
    await waitFor(() => expect(описание(screen.getByLabelText('Почта'))).toContain('Заполните'))
    expect(описание(screen.getByLabelText('Пароль'))).toContain('Заполните')
    // Первое сверху, а не первое попавшееся: человек читает форму
    // сверху вниз и правит её так же.
    expect(document.activeElement).toBe(screen.getByLabelText('Почта'))
  })

  // Порог длины здесь не проверить: jsdom не считает `tooShort`
  // (прогон 23.08.2026 — `validity.tooShort` остаётся false у поля
  // с minLength 8 и семью символами внутри). Пропуск назван вслух,
  // а сама длина проверяется в настоящем браузере — `e2e/a11y.spec.ts`.
  it.skip('короткий пароль отвергается своими словами', () => {})

  it('правка стирает отказ, не дожидаясь следующей отправки', async () => {
    const user = userEvent.setup()
    render(<Форма />)

    await user.click(screen.getByRole('button', { name: 'Войти' }))
    const почта = screen.getByLabelText('Почта')
    await waitFor(() => expect(почта.getAttribute('aria-invalid')).toBe('true'))

    // Прощение раннее, наказание позднее: отказ уходит с первым
    // исправленным символом, а новый может появиться только
    // на следующей отправке.
    await user.type(почта, 'a')
    expect(почта.getAttribute('aria-invalid')).toBeNull()
    expect(описание(почта)).not.toContain('Заполните')
  })

  it('форма проходится с клавиатуры до отказа сервера и обратно', async () => {
    const user = userEvent.setup()
    const сервер = vi
      .fn<(email: string) => Promise<void>>()
      .mockRejectedValueOnce(new Error('такая почта уже зарегистрирована — войдите'))
      .mockResolvedValueOnce(undefined)
    render(<Форма onSend={сервер} />)

    // Ни одного щелчка мышью дальше: форма заполняется, отправляется,
    // читается и правится с одной клавиатуры.
    await user.tab()
    expect(document.activeElement).toBe(screen.getByLabelText('Почта'))
    await user.keyboard('anna@example.test')
    await user.tab()
    expect(document.activeElement).toBe(screen.getByLabelText('Пароль'))
    await user.keyboard('parol12345{Enter}')

    // Отказ сервера встал у поля, к которому относится, и фокус —
    // на нём же: человеку не надо искать, что переписывать.
    await waitFor(() => expect(document.activeElement).toBe(screen.getByLabelText('Почта')))
    expect(описание(screen.getByLabelText('Почта'))).toContain('уже зарегистрирована')

    // И обратно: правка на месте, повторная отправка с той же
    // клавиатуры, отказ исчез.
    await user.keyboard('{Control>}a{/Control}vera@example.test{Enter}')
    await waitFor(() => expect(сервер).toHaveBeenCalledTimes(2))
    await waitFor(() =>
      expect(screen.getByLabelText('Почта').getAttribute('aria-invalid')).toBeNull(),
    )
  })

  it('отказ без своего поля объявляется сам', async () => {
    render(
      <FormError>неверная почта или пароль</FormError>,
    )
    // `role="alert"`, а не «просто красная строка»: у отказа уровня
    // формы нет поля, к которому мог бы уехать фокус, — значит,
    // он обязан объявить себя сам.
    expect(screen.getByRole('alert').textContent).toBe('неверная почта или пароль')
  })
})

/** Что диктор прочитает следом за подписью поля. */
function описание(поле: HTMLElement): string {
  const ids = поле.getAttribute('aria-describedby')?.split(' ') ?? []
  return ids
    .map((id) => document.getElementById(id)?.textContent ?? '')
    .join(' ')
}
