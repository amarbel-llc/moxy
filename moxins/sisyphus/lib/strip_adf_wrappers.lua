-- Pandoc Lua filter: convert marklas's ADF-only HTML wrappers to native
-- Markdown for human/LLM consumption.
--
-- Marklas emits ADF-only constructs (mention/status/inlineCard/panel) as
-- HTML with `adf=…` and `params=…` attributes so MD→ADF→MD round-trips
-- without losing structure. For one-shot reads (sisyphus's get-issue text
-- mode etc.) the wrappers are noise. This filter strips them, replacing
-- each construct with the closest native Markdown equivalent.
--
-- Pandoc parses marklas output (via gfm+raw_html) so that:
--   - `<a adf="inlineCard" href="X">Y</a>` becomes
--     [RawInline "<a adf=…>", Link …, RawInline "</a>"]
--   - `<span adf="mention" params=…>…</span>` becomes
--     [RawInline "<span adf=…>", Str/inlines, RawInline "</span>"]
--   - `<aside adf="panel" params=…>…</aside>` becomes
--     [RawBlock "<aside adf=…>", paragraphs, RawBlock "</aside>"]
--
-- The Inlines / Blocks filters below pattern-match those triplets and
-- rewrite them. See https://pandoc.org/lua-filters.html.

local function is_adf_open_inline(el)
  return el.t == "RawInline" and el.format == "html"
    and el.text:match('^<%a+%s+adf="') ~= nil
end

local function is_adf_close_inline(el)
  return el.t == "RawInline" and el.format == "html"
    and (el.text == "</a>" or el.text == "</span>")
end

local function is_adf_open_block(el)
  return el.t == "RawBlock" and el.format == "html"
    and el.text:match('^<%a+%s+adf="') ~= nil
end

local function is_adf_close_block(el)
  return el.t == "RawBlock" and el.format == "html"
    and el.text:match('^</%a+>%s*$') ~= nil
end

local function adf_kind(text)
  return text:match('adf="([^"]+)"')
end

local function panel_alert_prefix(text)
  -- Extract panelType from params='{"panelType":"info"}'.
  local pt = text:match('"panelType":"([^"]+)"')
  -- Map ADF panel types to GFM alert syntax. ADF supports info / note /
  -- warning / error / success. GFM defines NOTE / TIP / IMPORTANT /
  -- WARNING / CAUTION. Map conservatively; unknowns fall through to NOTE.
  local map = {
    info     = "NOTE",
    note     = "NOTE",
    success  = "TIP",
    warning  = "WARNING",
    error    = "CAUTION",
  }
  return map[pt or ""] or "NOTE"
end

-- Walk a list of inlines and strip adf-wrapper triplets.
function Inlines(inlines)
  local out = pandoc.Inlines({})
  local i = 1
  while i <= #inlines do
    local el = inlines[i]
    if is_adf_open_inline(el) then
      local kind = adf_kind(el.text)
      -- Find the matching close. Marklas always closes within the same
      -- list of inlines (no nesting across paragraphs for these
      -- constructs), so we scan forward.
      local close_at = nil
      for j = i + 1, #inlines do
        if is_adf_close_inline(inlines[j]) then
          close_at = j
          break
        end
      end
      if close_at then
        local inner = {}
        for j = i + 1, close_at - 1 do
          inner[#inner + 1] = inlines[j]
        end
        if kind == "inlineCard" then
          -- The href on the wrapping <a> is the same URL pandoc already
          -- recognised as an autolink Link inside `inner`. Just keep the
          -- inner contents — pandoc will render the Link as `[…](…)`.
          for _, x in ipairs(inner) do out:insert(x) end
        else
          -- mention, status, anything else — keep the inner text only.
          for _, x in ipairs(inner) do out:insert(x) end
        end
        i = close_at + 1
      else
        out:insert(el)
        i = i + 1
      end
    else
      out:insert(el)
      i = i + 1
    end
  end
  return out
end

-- Walk a list of blocks and convert <aside adf="panel">…</aside> blocks
-- into GFM alert blockquotes.
function Blocks(blocks)
  local out = pandoc.Blocks({})
  local i = 1
  while i <= #blocks do
    local el = blocks[i]
    if is_adf_open_block(el) then
      local kind = adf_kind(el.text)
      local close_at = nil
      for j = i + 1, #blocks do
        if is_adf_close_block(blocks[j]) then
          close_at = j
          break
        end
      end
      if close_at then
        local inner = {}
        for j = i + 1, close_at - 1 do
          inner[#inner + 1] = blocks[j]
        end
        if kind == "panel" then
          local alert = panel_alert_prefix(el.text)
          -- Prepend a paragraph carrying the GFM alert marker, then the
          -- inner blocks. Wrap the lot in a BlockQuote. The marker has to
          -- ride as RawInline (markdown format) so pandoc's writer doesn't
          -- escape the `[` and `]` — GFM alerts are a GitHub extension the
          -- gfm writer doesn't know natively.
          local bq_content = pandoc.Blocks({
            pandoc.Para({
              pandoc.RawInline("markdown", "[!" .. alert .. "]"),
            }),
          })
          for _, x in ipairs(inner) do bq_content:insert(x) end
          out:insert(pandoc.BlockQuote(bq_content))
        else
          -- Unknown ADF block wrapper — pass inner through.
          for _, x in ipairs(inner) do out:insert(x) end
        end
        i = close_at + 1
      else
        out:insert(el)
        i = i + 1
      end
    else
      out:insert(el)
      i = i + 1
    end
  end
  return out
end
