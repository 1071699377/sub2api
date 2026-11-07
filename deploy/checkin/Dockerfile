FROM python:3.12-alpine
WORKDIR /app
RUN apk add --no-cache tzdata
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY app.py .
COPY static ./static
ENV TZ=Asia/Shanghai STATIC_DIR=/app/static
EXPOSE 18100
CMD ["python", "-u", "app.py"]
